package client

import (
	"context"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/cschleiden/go-workflows/backend"
	a "github.com/cschleiden/go-workflows/internal/args"
	"github.com/cschleiden/go-workflows/internal/converter"
	"github.com/cschleiden/go-workflows/internal/core"
	"github.com/cschleiden/go-workflows/internal/fn"
	"github.com/cschleiden/go-workflows/internal/history"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pkg/errors"
)

var ErrWorkflowCanceled = errors.New("workflow canceled")
var ErrWorkflowTerminated = errors.New("workflow terminated")

type WorkflowInstanceOptions struct {
	InstanceID string
}

type Client interface {
	CreateWorkflowInstance(ctx context.Context, options WorkflowInstanceOptions, wf workflow.Workflow, args ...interface{}) (*workflow.Instance, error)

	CancelWorkflowInstance(ctx context.Context, instance *workflow.Instance) error

	WaitForWorkflowInstance(ctx context.Context, instance *workflow.Instance, timeout time.Duration) error

	SignalWorkflow(ctx context.Context, instanceID string, name string, arg interface{}) error
}

type client struct {
	backend backend.Backend
	clock   clock.Clock
}

func New(backend backend.Backend) Client {
	return &client{
		backend: backend,
		clock:   clock.New(),
	}
}

func (c *client) CreateWorkflowInstance(ctx context.Context, options WorkflowInstanceOptions, wf workflow.Workflow, args ...interface{}) (*workflow.Instance, error) {
	inputs, err := a.ArgsToInputs(converter.DefaultConverter, args...)
	if err != nil {
		return nil, errors.Wrap(err, "could not convert arguments")
	}

	startedEvent := history.NewHistoryEvent(
		c.clock.Now(),
		history.EventType_WorkflowExecutionStarted,
		&history.ExecutionStartedAttributes{
			Name:   fn.Name(wf),
			Inputs: inputs,
		})

	wfi := core.NewWorkflowInstance(options.InstanceID, uuid.NewString())

	startMessage := &history.WorkflowEvent{
		WorkflowInstance: wfi,
		HistoryEvent:     startedEvent,
	}

	if err := c.backend.CreateWorkflowInstance(ctx, *startMessage); err != nil {
		return nil, errors.Wrap(err, "could not create workflow instance")
	}

	return wfi, nil
}

func (c *client) CancelWorkflowInstance(ctx context.Context, instance *workflow.Instance) error {
	cancellationEvent := history.NewWorkflowCancellationEvent(time.Now())
	return c.backend.CancelWorkflowInstance(ctx, instance, &cancellationEvent)
}

func (c *client) SignalWorkflow(ctx context.Context, instanceID string, name string, arg interface{}) error {
	input, err := converter.DefaultConverter.To(arg)
	if err != nil {
		return errors.Wrap(err, "could not convert arguments")
	}

	event := history.NewHistoryEvent(
		c.clock.Now(),
		history.EventType_SignalReceived,
		&history.SignalReceivedAttributes{
			Name: name,
			Arg:  input,
		},
	)

	return c.backend.SignalWorkflow(ctx, instanceID, event)
}

func (c *client) WaitForWorkflowInstance(ctx context.Context, instance *workflow.Instance, timeout time.Duration) error {
	if timeout == 0 {
		timeout = time.Second * 20
	}

	ticker := c.clock.Ticker(time.Second)
	defer ticker.Stop()

	ctx, cancel := c.clock.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		s, err := c.backend.GetWorkflowInstanceState(ctx, instance)
		if err != nil {
			return errors.Wrap(err, "could not get workflow state")
		}

		if s == backend.WorkflowStateFinished {
			return nil
		}

		ticker.Reset(time.Second)
		select {
		case <-ticker.C:
			continue

		case <-ctx.Done():
			return errors.New("workflow did not finish in specified timeout")
		}
	}
}

func GetWorkflowResult[T any](ctx context.Context, c Client, instance *workflow.Instance, timeout time.Duration) (T, error) {
	// Zero result
	var z T

	if err := c.WaitForWorkflowInstance(ctx, instance, timeout); err != nil {
		return z, errors.Wrap(err, "workflow did not finish in time")
	}

	ic := c.(*client)
	b := ic.backend

	h, err := b.GetWorkflowInstanceHistory(ctx, instance)
	if err != nil {
		return z, errors.Wrap(err, "could not get workflow history")
	}

	// Iterate over history backwards
	for i := len(h) - 1; i >= 0; i-- {
		event := h[i]
		switch event.Type {
		case history.EventType_WorkflowExecutionFinished:
			a := event.Attributes.(*history.ExecutionCompletedAttributes)
			if a.Error != "" {
				return z, errors.New(a.Error)
			}

			var r T
			if err := converter.DefaultConverter.From(a.Result, &r); err != nil {
				return z, errors.Wrap(err, "could not convert result")
			}

			return r, nil

		case history.EventType_WorkflowExecutionCanceled:
			return z, ErrWorkflowCanceled

		case history.EventType_WorkflowExecutionTerminated:
			return z, ErrWorkflowTerminated
		}
	}

	return z, errors.New("workflow finished, but could not find result event")
}