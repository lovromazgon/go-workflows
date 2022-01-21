package main

import (
	"context"
	"log"
	"os"

	"github.com/cschleiden/go-dt/pkg/backend"
	"github.com/cschleiden/go-dt/pkg/backend/mysql"
	"github.com/cschleiden/go-dt/pkg/client"
	"github.com/cschleiden/go-dt/pkg/worker"
	"github.com/cschleiden/go-dt/pkg/workflow"
	"github.com/google/uuid"
)

func main() {
	ctx := context.Background()

	// b := sqlite.NewSqliteBackend("simple.sqlite")
	//b := memory.NewMemoryBackend()
	b := mysql.NewMysqlBackend("root", "SqlPassw0rd", "simple")

	// Run worker
	go RunWorker(ctx, b)

	// Start workflow via client
	c := client.New(b)

	startWorkflow(ctx, c)
	// startWorkflow(ctx, c)

	c2 := make(chan os.Signal, 1)
	<-c2
}

func startWorkflow(ctx context.Context, c client.Client) {
	wf, err := c.CreateWorkflowInstance(ctx, client.WorkflowInstanceOptions{
		InstanceID: uuid.NewString(),
	}, Workflow1, Workflow1Args{
		Name: "John",
		Age:  35,
	})
	if err != nil {
		log.Fatal(err)
		panic("could not start workflow")
	}

	log.Println("Started workflow", wf.GetInstanceID())
}

func RunWorker(ctx context.Context, mb backend.Backend) {
	w := worker.New(mb)

	w.RegisterWorkflow(Workflow1)

	w.RegisterActivity(Activity1)
	w.RegisterActivity(Activity2)

	if err := w.Start(ctx); err != nil {
		panic("could not start worker")
	}
}

type Workflow1Args struct {
	Name string
	Age  int
}

func Workflow1(ctx workflow.Context, args Workflow1Args) error {
	log.Println("Entering Workflow1")
	log.Println("\tWorkflow instance input:", args.Name, "age:", args.Age)
	log.Println("\tIsReplaying:", workflow.Replaying(ctx))

	defer func() {
		log.Println("Leaving Workflow1")
	}()

	var r1, r2 int
	err := workflow.ExecuteActivity(ctx, Activity1, 35, 12).Get(ctx, &r1)
	if err != nil {
		panic("error getting activity 1 result")
	}
	log.Println("R1 result:", r1)

	log.Println("\tIsReplaying:", workflow.Replaying(ctx))

	err = workflow.ExecuteActivity(ctx, Activity2).Get(ctx, &r2)
	if err != nil {
		panic("error getting activity 1 result")
	}
	log.Println("R2 result:", r2)

	return nil
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

	defer func() {
		log.Println("Leaving Activity2")
	}()

	return 12, nil
}