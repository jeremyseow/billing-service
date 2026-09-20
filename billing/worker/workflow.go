package worker

import (
	"time"

	"billing-service/billing/rates"

	"github.com/shopspring/decimal"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

var activities *Activities

type BillingWorkflowParams struct {
	BillID             string    `json:"bill_id"`
	AccountID          string    `json:"account_id"`
	PeriodStart        time.Time `json:"period_start"`
	PeriodEnd          time.Time `json:"period_end"`
	SettlementCurrency string    `json:"settlement_currency"`
}

type AddLineItemInput struct {
	IdempotencyKey string          `json:"idempotency_key"`
	Description    string          `json:"description"`
	Amount         decimal.Decimal `json:"amount"`
	Currency       string          `json:"currency"`
}

type AddLineItemResult struct {
	OriginalTotals  map[string]decimal.Decimal `json:"original_totals"`
	SettlementTotal decimal.Decimal            `json:"settlement_total"`
}

type CloseBillResult struct {
	OriginalTotals  map[string]decimal.Decimal `json:"original_totals"`
	SettlementTotal decimal.Decimal            `json:"settlement_total"`
}

type WorkflowState struct {
	Status               string                     `json:"status"`
	OriginalTotals       map[string]decimal.Decimal `json:"original_totals"`
	SettlementTotal      decimal.Decimal            `json:"settlement_total"`
	ProcessedIdempotency map[string]bool            `json:"processed_idempotency"`
	ManualCloseTriggered bool                       `json:"manual_close_triggered"`
	IsTerminated         bool                       `json:"is_terminated"`
}

func BillingWorkflow(ctx workflow.Context, params BillingWorkflowParams) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting BillingWorkflow", "WorkflowID", workflow.GetInfo(ctx).WorkflowExecution.ID, "AccountID", params.AccountID, "PeriodStart", params.PeriodStart, "PeriodEnd", params.PeriodEnd)

	state := WorkflowState{
		Status:               "OPEN",
		OriginalTotals:       make(map[string]decimal.Decimal),
		SettlementTotal:      decimal.Zero,
		ProcessedIdempotency: make(map[string]bool),
		ManualCloseTriggered: false,
		IsTerminated:         false,
	}

	// Register synchronous update handler for AddLineItem
	err := workflow.SetUpdateHandler(ctx, "AddLineItem", func(ctx workflow.Context, input AddLineItemInput) (AddLineItemResult, error) {
		logger.Info("Received AddLineItem update request", "IdempotencyKey", input.IdempotencyKey, "Currency", input.Currency, "Amount", input.Amount)

		if state.Status != "OPEN" {
			logger.Warn("Rejecting AddLineItem: bill is closed", "BillID", params.BillID)
			return AddLineItemResult{}, temporal.NewApplicationError("bill is closed", "ErrBillClosed")
		}

		if input.Currency != "USD" && input.Currency != "GEL" {
			logger.Warn("Rejecting AddLineItem: invalid currency", "Currency", input.Currency)
			return AddLineItemResult{}, temporal.NewApplicationError("invalid currency", "ErrInvalidCurrency")
		}

		if state.ProcessedIdempotency[input.IdempotencyKey] {
			logger.Info("AddLineItem: duplicate key ignored (idempotent)", "IdempotencyKey", input.IdempotencyKey)
			return AddLineItemResult{OriginalTotals: state.OriginalTotals, SettlementTotal: state.SettlementTotal}, nil
		}

		// Execute activity to persist the line item in PostgreSQL
		ao := workflow.ActivityOptions{
			StartToCloseTimeout: 10 * time.Second,
			RetryPolicy: &temporal.RetryPolicy{
				InitialInterval:    time.Second,
				BackoffCoefficient: 2.0,
				MaximumInterval:    10 * time.Second,
			},
		}
		ctxWithAO := workflow.WithActivityOptions(ctx, ao)

		err := workflow.ExecuteActivity(ctxWithAO, activities.PersistLineItemActivity, PersistLineItemParams{
			ID:                 workflow.GetInfo(ctx).WorkflowExecution.ID + ":" + input.IdempotencyKey, // unique row primary key
			BillID:             params.BillID,
			IdempotencyKey:     input.IdempotencyKey,
			Description:        input.Description,
			Amount:             input.Amount,
			Currency:           input.Currency,
			SettlementCurrency: params.SettlementCurrency,
		}).Get(ctx, nil)
		if err != nil {
			logger.Error("PersistLineItemActivity failed", "Error", err)
			return AddLineItemResult{}, err
		}

		// Update in-memory state
		state.ProcessedIdempotency[input.IdempotencyKey] = true
		state.OriginalTotals[input.Currency] = state.OriginalTotals[input.Currency].Add(input.Amount)

		// Calculate settlement conversion for running settlement total
		settlementAmount, _, _ := rates.ConvertRates(input.Currency, params.SettlementCurrency, input.Amount)
		state.SettlementTotal = state.SettlementTotal.Add(settlementAmount)

		logger.Info("Successfully processed line item", "IdempotencyKey", input.IdempotencyKey, "OriginalTotals", state.OriginalTotals, "SettlementTotal", state.SettlementTotal)
		return AddLineItemResult{OriginalTotals: state.OriginalTotals, SettlementTotal: state.SettlementTotal}, nil
	})
	if err != nil {
		return err
	}

	// Register synchronous update handler for CloseBill
	err = workflow.SetUpdateHandler(ctx, "CloseBill", func(ctx workflow.Context) (CloseBillResult, error) {
		logger.Info("Received CloseBill update request", "BillID", params.BillID)
		if state.Status != "OPEN" {
			return CloseBillResult{OriginalTotals: state.OriginalTotals, SettlementTotal: state.SettlementTotal}, nil
		}
		state.ManualCloseTriggered = true
		return CloseBillResult{OriginalTotals: state.OriginalTotals, SettlementTotal: state.SettlementTotal}, nil
	})
	if err != nil {
		return err
	}

	// Register synchronous update handler for TerminateBill
	err = workflow.SetUpdateHandler(ctx, "TerminateBill", func(ctx workflow.Context) (CloseBillResult, error) {
		logger.Info("Received TerminateBill update request", "BillID", params.BillID)
		if state.Status != "OPEN" {
			return CloseBillResult{OriginalTotals: state.OriginalTotals, SettlementTotal: state.SettlementTotal}, nil
		}
		state.ManualCloseTriggered = true
		state.IsTerminated = true
		return CloseBillResult{OriginalTotals: state.OriginalTotals, SettlementTotal: state.SettlementTotal}, nil
	})
	if err != nil {
		return err
	}

	// Wait until period_end or manual close/termination triggered
	now := workflow.Now(ctx)
	sleepDuration := params.PeriodEnd.Sub(now)
	if sleepDuration < 0 {
		sleepDuration = 0
	}

	logger.Info("Awaiting bill closure or timeout", "TimeoutDuration", sleepDuration)
	_, _ = workflow.AwaitWithTimeout(ctx, sleepDuration, func() bool {
		return state.ManualCloseTriggered || state.IsTerminated
	})

	// Transition to CLOSED or TERMINATED
	targetStatus := "CLOSED"
	if state.IsTerminated {
		targetStatus = "TERMINATED"
	}
	state.Status = targetStatus
	logger.Info("Transitioning bill to finalized state", "BillID", params.BillID, "Status", targetStatus)

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    10 * time.Second,
		},
	}
	ctxWithAO := workflow.WithActivityOptions(ctx, ao)

	// Execute CloseBillActivity to persist totals and final status
	err = workflow.ExecuteActivity(ctxWithAO, activities.CloseBillActivity, CloseBillParams{
		BillID:   params.BillID,
		ClosedAt: workflow.Now(ctx),
		Totals:   state.OriginalTotals,
		Status:   targetStatus,
	}).Get(ctx, nil)
	if err != nil {
		logger.Error("CloseBillActivity failed", "Error", err)
		return err
	}

	// Trigger next billing cycle automatically only if not terminated
	if !state.IsTerminated {
		nextPeriodStart := params.PeriodEnd
		nextPeriodEnd := nextPeriodStart.Add(params.PeriodEnd.Sub(params.PeriodStart))

		logger.Info("Scheduling next billing cycle", "NextStart", nextPeriodStart, "NextEnd", nextPeriodEnd)
		err = workflow.ExecuteActivity(ctxWithAO, activities.StartNextBillingCycleActivity, StartNextBillingCycleParams{
			AccountID:          params.AccountID,
			PeriodStart:        nextPeriodStart,
			PeriodEnd:          nextPeriodEnd,
			SettlementCurrency: params.SettlementCurrency,
		}).Get(ctx, nil)
		if err != nil {
			logger.Error("StartNextBillingCycleActivity failed", "Error", err)
			return err
		}
	}

	logger.Info("BillingWorkflow completed successfully", "BillID", params.BillID)
	return nil
}
