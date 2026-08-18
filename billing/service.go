package billing

import (
	"context"
	"fmt"
	"time"

	"billing-service/billing/domain"
	"billing-service/billing/repository"
	"billing-service/billing/worker"

	"encore.dev/rlog"
	"encore.dev/storage/sqldb"
	"go.temporal.io/sdk/client"
	tworker "go.temporal.io/sdk/worker"
)

var db = sqldb.Named("billing")

//encore:service
type Service struct {
	client client.Client
	worker tworker.Worker
	repo   domain.Repository
}

func initService() (*Service, error) {
	var err error
	var c client.Client

	// Retry loop for Temporal server connection (handles startup race conditions with docker-compose)
	for i := range 30 {
		c, err = client.Dial(client.Options{
			HostPort: "localhost:7233",
		})
		if err == nil {
			break
		}
		rlog.Warn("Failed to connect to Temporal, retrying...", "Attempt", i+1, "Error", err)
		time.Sleep(1 * time.Second)
	}

	if err != nil {
		return nil, fmt.Errorf("could not connect to Temporal: %w", err)
	}

	// Instantiate the Postgres Repository
	repo := repository.NewPostgresRepository(db)

	// Instantiate Activities with injected dependencies
	act := worker.NewActivities(repo, c)

	w := tworker.New(c, "billing-queue", tworker.Options{})
	w.RegisterWorkflow(worker.BillingWorkflow)
	w.RegisterActivity(act.PersistLineItemActivity)
	w.RegisterActivity(act.CloseBillActivity)
	w.RegisterActivity(act.StartNextBillingCycleActivity)

	if err := w.Start(); err != nil {
		c.Close()
		return nil, fmt.Errorf("failed to start Temporal worker: %w", err)
	}

	rlog.Info("Temporal worker started, listening on queue: billing-queue")

	return &Service{
		client: c,
		worker: w,
		repo:   repo,
	}, nil
}

// Shutdown gracefully stops the worker and closes the Temporal client.
func (s *Service) Shutdown(force context.Context) {
	s.worker.Stop()
	s.client.Close()
}
