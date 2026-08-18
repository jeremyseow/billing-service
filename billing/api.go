package billing

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"pave-bank/billing/domain"
	"pave-bank/billing/worker"
	"encore.dev/beta/errs"
	"encore.dev/rlog"
	"github.com/google/uuid"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

type CreateBillParams struct {
	AccountID          string    `json:"account_id"`
	PeriodStart        time.Time `json:"period_start"`
	PeriodEnd          time.Time `json:"period_end"`
	SettlementCurrency string    `json:"settlement_currency"`
}

type BillResponse struct {
	ID                 string     `json:"id"`
	AccountID          string     `json:"account_id"`
	WorkflowID         string     `json:"workflow_id"`
	PeriodStart        time.Time  `json:"period_start"`
	PeriodEnd          time.Time  `json:"period_end"`
	Status             string     `json:"status"`
	SettlementCurrency string     `json:"settlement_currency"`
	ClosedAt           *time.Time `json:"closed_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// CreateBill initiates a billing workflow and records the bill.
//
//encore:api public path=/bills method=POST
func (s *Service) CreateBill(ctx context.Context, params *CreateBillParams) (*BillResponse, error) {
	rlog.Info("CreateBill endpoint called", "AccountID", params.AccountID, "PeriodStart", params.PeriodStart)

	generatedID := uuid.NewString()

	bill := &domain.Bill{
		ID:                 generatedID,
		AccountID:          params.AccountID,
		PeriodStart:        params.PeriodStart,
		PeriodEnd:          params.PeriodEnd,
		Status:             "OPEN",
		SettlementCurrency: params.SettlementCurrency,
	}

	if err := bill.Validate(); err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}

	// Check if the account already has an active OPEN bill with a different period start
	activeExists, err := s.repo.CheckActiveBillExists(ctx, params.AccountID, params.PeriodStart)
	if err != nil {
		rlog.Error("Database query error in CreateBill checking active bill", "Error", err)
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to check active bills"}
	}
	if activeExists {
		return nil, &errs.Error{Code: errs.AlreadyExists, Message: "account already has an active open bill"}
	}

	// Insert the bill using repo (handles unique constraint conflict resolution internally)
	err = s.repo.InsertBill(ctx, bill)
	if err != nil {
		rlog.Error("Database insertion error in CreateBill", "Error", err)
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to create bill record"}
	}

	// Start Temporal workflow only if the bill was newly created (idempotent duplicate inserts will return the existing bill.ID)
	if bill.ID == generatedID {
		if s.client == nil {
			rlog.Error("Temporal client is not initialized")
			return nil, &errs.Error{Code: errs.Unavailable, Message: "temporal client not ready"}
		}

		options := client.StartWorkflowOptions{
			ID:        bill.ID,
			TaskQueue: "billing-queue",
		}

		_, err = s.client.ExecuteWorkflow(ctx, options, worker.BillingWorkflow, worker.BillingWorkflowParams{
			BillID:             bill.ID,
			AccountID:          bill.AccountID,
			PeriodStart:        bill.PeriodStart,
			PeriodEnd:          bill.PeriodEnd,
			SettlementCurrency: bill.SettlementCurrency,
		})
		if err != nil {
			rlog.Error("Failed to execute Temporal BillingWorkflow", "Error", err)
			return nil, &errs.Error{Code: errs.Internal, Message: "failed to start billing workflow"}
		}

		rlog.Info("Successfully started BillingWorkflow", "WorkflowID", bill.ID, "BillID", bill.ID)
	} else {
		rlog.Info("Bill already exists, returning existing bill record", "BillID", bill.ID)
	}

	return &BillResponse{
		ID:                 bill.ID,
		AccountID:          bill.AccountID,
		WorkflowID:         bill.ID, // UUID is the workflow ID
		PeriodStart:        bill.PeriodStart,
		PeriodEnd:          bill.PeriodEnd,
		Status:             bill.Status,
		SettlementCurrency: bill.SettlementCurrency,
		ClosedAt:           bill.ClosedAt,
		CreatedAt:          bill.CreatedAt,
		UpdatedAt:          bill.UpdatedAt,
	}, nil
}

type AddLineItemParams struct {
	IdempotencyKey string `json:"idempotency_key"`
	Description    string `json:"description"`
	AmountMinor    int64  `json:"amount_minor"`
	Currency       string `json:"currency"`
}

type AddLineItemResponse struct {
	Totals map[string]int64 `json:"totals"`
}

// AddLineItem dispatches the AddLineItem Temporal Update.
//
//encore:api public path=/bills/:id/items method=POST
func (s *Service) AddLineItem(ctx context.Context, id string, params *AddLineItemParams) (*AddLineItemResponse, error) {
	rlog.Info("AddLineItem endpoint called", "BillID", id, "IdempotencyKey", params.IdempotencyKey)

	if params.Currency != "USD" && params.Currency != "GEL" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid currency"}
	}

	if s.client == nil {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "temporal client not ready"}
	}

	// Synchronously send update to Temporal workflow
	updateHandle, err := s.client.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   id,
		UpdateName:   "AddLineItem",
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args: []interface{}{worker.AddLineItemInput{
			IdempotencyKey: params.IdempotencyKey,
			Description:    params.Description,
			AmountMinor:    params.AmountMinor,
			Currency:       params.Currency,
		}},
	})
	if err != nil {
		rlog.Error("Failed to update Temporal workflow", "WorkflowID", id, "Error", err)
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to communicate with workflow"}
	}

	var result worker.AddLineItemResult
	err = updateHandle.Get(ctx, &result)
	if err != nil {
		var appErr *temporal.ApplicationError
		if errors.As(err, &appErr) {
			if appErr.Type() == "ErrBillClosed" {
				return nil, &errs.Error{Code: errs.InvalidArgument, Message: "bill is closed"}
			}
			if appErr.Type() == "ErrInvalidCurrency" {
				return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid currency"}
			}
		}
		rlog.Error("Update handler returned application error", "Error", err)
		return nil, &errs.Error{Code: errs.Internal, Message: err.Error()}
	}

	return &AddLineItemResponse{Totals: result.Totals}, nil
}

type CloseBillResponse struct {
	Totals map[string]int64 `json:"totals"`
}

// CloseBill dispatches the CloseBill Temporal Update manually.
//
//encore:api public path=/bills/:id/close method=POST
func (s *Service) CloseBill(ctx context.Context, id string) (*CloseBillResponse, error) {
	rlog.Info("CloseBill manual trigger endpoint called", "BillID", id)

	if s.client == nil {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "temporal client not ready"}
	}

	updateHandle, err := s.client.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   id,
		UpdateName:   "CloseBill",
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		rlog.Error("Failed to dispatch CloseBill update", "WorkflowID", id, "Error", err)
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to send close update to workflow"}
	}

	var result worker.CloseBillResult
	err = updateHandle.Get(ctx, &result)
	if err != nil {
		rlog.Error("CloseBill update execution error", "Error", err)
		return nil, &errs.Error{Code: errs.Internal, Message: err.Error()}
	}

	return &CloseBillResponse{Totals: result.Totals}, nil
}

type TerminateBillResponse struct {
	Totals map[string]int64 `json:"totals"`
}

// TerminateBill dispatches the TerminateBill Temporal Update to close the bill permanently.
//
//encore:api public path=/bills/:id/terminate method=POST
func (s *Service) TerminateBill(ctx context.Context, id string) (*TerminateBillResponse, error) {
	rlog.Info("TerminateBill manual trigger endpoint called", "BillID", id)

	if s.client == nil {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "temporal client not ready"}
	}

	updateHandle, err := s.client.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   id,
		UpdateName:   "TerminateBill",
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		rlog.Error("Failed to dispatch TerminateBill update", "WorkflowID", id, "Error", err)
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to send terminate update to workflow"}
	}

	var result worker.CloseBillResult
	err = updateHandle.Get(ctx, &result)
	if err != nil {
		rlog.Error("TerminateBill update execution error", "Error", err)
		return nil, &errs.Error{Code: errs.Internal, Message: err.Error()}
	}

	return &TerminateBillResponse{Totals: result.Totals}, nil
}

// GetBill queries PostgreSQL directly joining bills, bill_totals, and line_items.
//
//encore:api public path=/bills/:id method=GET
func (s *Service) GetBill(ctx context.Context, id string) (*domain.BillSummary, error) {
	rlog.Info("GetBill endpoint called", "BillID", id)

	summary, err := s.repo.GetBillSummary(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			rlog.Warn("Bill not found for retrieval", "BillID", id)
			return nil, &errs.Error{Code: errs.NotFound, Message: "bill not found"}
		}
		rlog.Error("Error querying bill summary", "Error", err)
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to retrieve bill totals"}
	}

	return summary, nil
}
