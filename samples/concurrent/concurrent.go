package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/cschleiden/go-dt/pkg/backend"
	"github.com/cschleiden/go-dt/pkg/backend/memory"
	"github.com/cschleiden/go-dt/pkg/client"
	"github.com/cschleiden/go-dt/pkg/sync"
	"github.com/cschleiden/go-dt/pkg/worker"
	"github.com/cschleiden/go-dt/pkg/workflow"
	"github.com/google/uuid"
)

func main() {
	ctx := context.Background()

	mb := memory.NewMemoryBackend()

	// Run worker
	go RunWorker(ctx, mb)

	// Start workflow via client
	c := client.NewClient(mb)

	startWorkflow(ctx, c)

	c2 := make(chan os.Signal, 1)
	<-c2
}

func startWorkflow(ctx context.Context, c client.Client) {
	wf, err := c.CreateWorkflowInstance(ctx, client.WorkflowInstanceOptions{
		InstanceID: uuid.NewString(),
	}, Workflow1, "Hello world")
	if err != nil {
		panic("could not start workflow")
	}

	log.Println("Started workflow", wf.GetInstanceID())
}

func RunWorker(ctx context.Context, mb backend.Backend) {
	w := worker.NewWorker(mb)

	w.RegisterWorkflow("wf1", Workflow1)

	w.RegisterActivity("a1", Activity1)
	w.RegisterActivity("a2", Activity2)

	if err := w.Start(ctx); err != nil {
		panic("could not start worker")
	}
}

func Workflow1(ctx context.Context, msg string) (string, error) {
	log.Println("Entering Workflow1")
	log.Println("\tWorkflow instance input:", msg)
	log.Println("\tIsReplaying:", workflow.Replaying(ctx))

	defer func() {
		log.Println("Leaving Workflow1")
	}()

	a1, err := workflow.ExecuteActivity(ctx, "a1", 35, 12)
	if err != nil {
		panic("error executing activity 1")
	}

	a2, err := workflow.ExecuteActivity(ctx, "a2")
	if err != nil {
		panic("error executing activity 1")
	}

	results := 0

	s := workflow.NewSelector()

	s.AddFuture(a2, func(ctx context.Context, f2 sync.Future) {
		var r int
		if err := f2.Get(ctx, &r); err != nil {
			panic(err)
		}

		log.Println("A2 result", r)
		results++
	})

	s.AddFuture(a1, func(ctx context.Context, f1 sync.Future) {
		var r int
		if err := f1.Get(ctx, &r); err != nil {
			panic(err)
		}

		log.Println("A1 result", r)

		results++
	})

	for results < 2 {
		log.Println("Selecting...")
		log.Println("\tIsReplaying:", workflow.Replaying(ctx))
		s.Select(ctx)
		log.Println("Selected")
		log.Println("\tIsReplaying:", workflow.Replaying(ctx))
	}

	return "result", nil
}

func Activity1(ctx context.Context, a, b int) (int, error) {
	log.Println("Entering Activity1")

	defer func() {
		log.Println("Leaving Activity1")
	}()

	return a + b, nil
}

func Activity2(ctx context.Context) (int, error) {
	log.Println("Entering Activity2")

	time.Sleep(5 * time.Second)

	defer func() {
		log.Println("Leaving Activity2")
	}()

	return 12, nil
}