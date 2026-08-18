package worker

import (
	"context"
	"fmt"
	"time"

	"billing-service/billing/domain"
	"encore.dev/rlog"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"go.temporal.io/sdk/client"
)

var (
	activeWorkflowsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "active_billing_workflows",
		Help: "Number of active billing workflows currently running.",
	})
	lineItemsProcessedCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "billing_line_items_total",
		Help: "Total number of line items processed by the billing service.",
	}, []string{"currency"})
	billingFailuresCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "billing_failures_total",
		Help: "Total number of failures encountered during billing operations.",
	}, []string{"operation"})
)

func init() {
	prometheus.MustRegister(activeWorkflowsGauge)
	prometheus.MustRegister(lineItemsProcessedCounter)
	prometheus.MustRegister(billingFailuresCounter)
}

type Activities struct {
	Repo           domain.Repository
	TemporalClient client.Client
}

func NewActivities(repo domain.Repository, temporalClient client.Client) *Activities {
	return &Activities{
		Repo:           repo,
		TemporalClient: temporalClient,
	}
}

type PersistLineItemParams struct {
	ID             string `json:"id"`
	BillID         string `json:"bill_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Description    string `json:"description"`
	AmountMinor    int64  `json:"amount_minor"`
	Currency       string `json:"currency"`
}

func (a *Activities) PersistLineItemActivity(ctx context.Context, params PersistLineItemParams) error {
	rlog.Info("Executing PersistLineItemActivity", "BillID", params.BillID, "IdempotencyKey", params.IdempotencyKey)

	item := &domain.LineItem{
		ID:             params.ID,
		BillID:         params.BillID,
		IdempotencyKey: params.IdempotencyKey,
		Description:    params.Description,
		AmountMinor:    params.AmountMinor,
		Currency:       params.Currency,
	}

	err := a.Repo.SaveLineItem(ctx, item)
	if err != nil {
		rlog.Error("Failed to persist line item in database", "Error", err)
		billingFailuresCounter.WithLabelValues("persist_line_item").Inc()
		return err
	}

	// Increment metric for processed line items
	lineItemsProcessedCounter.WithLabelValues(params.Currency).Inc()
	return nil
}

type CloseBillParams struct {
	BillID   string           `json:"bill_id"`
	ClosedAt time.Time        `json:"closed_at"`
	Totals   map[string]int64 `json:"totals"`
	Status   string           `json:"status"`
}

func (a *Activities) CloseBillActivity(ctx context.Context, params CloseBillParams) error {
	rlog.Info("Executing CloseBillActivity", "BillID", params.BillID, "Status", params.Status)

	err := a.Repo.CloseBill(ctx, params.BillID, params.Status, params.ClosedAt, params.Totals)
	if err != nil {
		rlog.Error("Failed to close bill in database", "Error", err)
		billingFailuresCounter.WithLabelValues("close_bill").Inc()
		return err
	}

	// Decrement active workflow count
	activeWorkflowsGauge.Dec()
	rlog.Info("Successfully closed bill and snapshotted totals in database", "BillID", params.BillID)
	return nil
}

type StartNextBillingCycleParams struct {
	AccountID          string    `json:"account_id"`
	PeriodStart        time.Time `json:"period_start"`
	PeriodEnd          time.Time `json:"period_end"`
	SettlementCurrency string    `json:"settlement_currency"`
}

func (a *Activities) StartNextBillingCycleActivity(ctx context.Context, params StartNextBillingCycleParams) error {
	rlog.Info("Executing StartNextBillingCycleActivity", "AccountID", params.AccountID, "NextPeriodStart", params.PeriodStart)

	if a.TemporalClient == nil {
		err := fmt.Errorf("temporal client is not initialized")
		rlog.Error("StartNextBillingCycleActivity initialization error", "Error", err)
		billingFailuresCounter.WithLabelValues("temporal_client_uninitialized").Inc()
		return err
	}

	nextBillID := uuid.NewString()

	// Persist the new bill row as OPEN
	bill := &domain.Bill{
		ID:                 nextBillID,
		AccountID:          params.AccountID,
		PeriodStart:        params.PeriodStart,
		PeriodEnd:          params.PeriodEnd,
		Status:             "OPEN",
		SettlementCurrency: params.SettlementCurrency,
	}

	err := a.Repo.InsertBill(ctx, bill)
	if err != nil {
		rlog.Error("Failed to insert next billing cycle bill", "Error", err)
		billingFailuresCounter.WithLabelValues("next_cycle_db_insert").Inc()
		return err
	}

	options := client.StartWorkflowOptions{
		ID:        nextBillID,
		TaskQueue: "billing-queue",
	}

	rlog.Info("Starting next cycle workflow", "WorkflowID", nextBillID)
	_, err = a.TemporalClient.ExecuteWorkflow(ctx, options, BillingWorkflow, BillingWorkflowParams{
		BillID:             nextBillID,
		AccountID:          params.AccountID,
		PeriodStart:        params.PeriodStart,
		PeriodEnd:          params.PeriodEnd,
		SettlementCurrency: params.SettlementCurrency,
	})
	if err != nil {
		rlog.Error("Failed to trigger next cycle Temporal workflow", "Error", err)
		billingFailuresCounter.WithLabelValues("next_cycle_workflow_trigger").Inc()
		return err
	}

	// Increment active workflow count
	activeWorkflowsGauge.Inc()
	rlog.Info("Successfully triggered next billing cycle workflow", "WorkflowID", nextBillID)
	return nil
}
