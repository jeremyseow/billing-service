package billing

import (
	"context"
	"testing"
	"time"

	"billing-service/billing/repository"
	"billing-service/billing/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
)

func TestBillingWorkflow_SuccessfulCycle(t *testing.T) {
	s := &testsuite.WorkflowTestSuite{}
	env := s.NewTestWorkflowEnvironment()

	// Mock activities
	var act *worker.Activities
	env.OnActivity(act.PersistLineItemActivity, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(act.CloseBillActivity, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(act.StartNextBillingCycleActivity, mock.Anything, mock.Anything).Return(nil)

	params := worker.BillingWorkflowParams{
		BillID:             "test-bill-id",
		AccountID:          "test-account-id",
		PeriodStart:        time.Now(),
		PeriodEnd:          time.Now().Add(24 * time.Hour),
		SettlementCurrency: "USD",
	}

	// State variables to capture asynchronous update results
	var add1Result worker.AddLineItemResult
	var add1Err error

	var add2Result worker.AddLineItemResult
	var add2Err error

	var add3Result worker.AddLineItemResult
	var add3Err error

	var add4Err error

	var closeResult worker.CloseBillResult
	var closeErr error

	// At 1 hour: Send first line item (USD)
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow("AddLineItem", "update-id-1", &testsuite.TestUpdateCallback{
			OnComplete: func(val interface{}, err error) {
				if err == nil {
					add1Result = val.(worker.AddLineItemResult)
				}
				add1Err = err
			},
		}, worker.AddLineItemInput{
			IdempotencyKey: "item-1",
			Description:    "valid item USD",
			AmountMinor:    100,
			Currency:       "USD",
		})
	}, 1*time.Hour)

	// At 1 hour + 5 seconds: Verify first, send second (GEL credit)
	env.RegisterDelayedCallback(func() {
		assert.NoError(t, add1Err)
		assert.Equal(t, int64(100), add1Result.Totals["USD"])

		env.UpdateWorkflow("AddLineItem", "update-id-2", &testsuite.TestUpdateCallback{
			OnComplete: func(val interface{}, err error) {
				if err == nil {
					add2Result = val.(worker.AddLineItemResult)
				}
				add2Err = err
			},
		}, worker.AddLineItemInput{
			IdempotencyKey: "item-2",
			Description:    "gel credit",
			AmountMinor:    -50,
			Currency:       "GEL",
		})
	}, 1*time.Hour+5*time.Second)

	// At 1 hour + 10 seconds: Verify second, send third (duplicate item-1)
	env.RegisterDelayedCallback(func() {
		assert.NoError(t, add2Err)
		assert.Equal(t, int64(-50), add2Result.Totals["GEL"])

		env.UpdateWorkflow("AddLineItem", "update-id-3", &testsuite.TestUpdateCallback{
			OnComplete: func(val interface{}, err error) {
				if err == nil {
					add3Result = val.(worker.AddLineItemResult)
				}
				add3Err = err
			},
		}, worker.AddLineItemInput{
			IdempotencyKey: "item-1", // duplicate key
			Description:    "duplicate item USD",
			AmountMinor:    200,
			Currency:       "USD",
		})
	}, 1*time.Hour+10*time.Second)

	// At 1 hour + 15 seconds: Verify third (deduplicated), send fourth (invalid currency EUR)
	env.RegisterDelayedCallback(func() {
		assert.NoError(t, add3Err)
		assert.Equal(t, int64(100), add3Result.Totals["USD"]) // stays 100

		env.UpdateWorkflow("AddLineItem", "update-id-4", &testsuite.TestUpdateCallback{
			OnComplete: func(val interface{}, err error) {
				add4Err = err
			},
			OnReject: func(err error) {
				add4Err = err
			},
		}, worker.AddLineItemInput{
			IdempotencyKey: "item-4",
			Description:    "invalid currency",
			AmountMinor:    150,
			Currency:       "EUR",
		})
	}, 1*time.Hour+15*time.Second)

	// At 1 hour + 20 seconds: Verify fourth rejected, send close trigger
	env.RegisterDelayedCallback(func() {
		assert.Error(t, add4Err)

		env.UpdateWorkflow("CloseBill", "update-id-5", &testsuite.TestUpdateCallback{
			OnComplete: func(val interface{}, err error) {
				if err == nil {
					closeResult = val.(worker.CloseBillResult)
				}
				closeErr = err
			},
		})
	}, 1*time.Hour+20*time.Second)

	// At 1 hour + 25 seconds: Verify close and closed totals
	env.RegisterDelayedCallback(func() {
		assert.NoError(t, closeErr)
		assert.Equal(t, int64(100), closeResult.Totals["USD"])
		assert.Equal(t, int64(-50), closeResult.Totals["GEL"])
	}, 1*time.Hour+25*time.Second)

	env.ExecuteWorkflow(worker.BillingWorkflow, params)
	assert.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
}

func TestBillingWorkflow_ClosedBillRejection(t *testing.T) {
	s := &testsuite.WorkflowTestSuite{}
	env := s.NewTestWorkflowEnvironment()

	// Mock activities
	var act *worker.Activities
	env.OnActivity(act.PersistLineItemActivity, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(act.CloseBillActivity, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(act.StartNextBillingCycleActivity, mock.Anything, mock.Anything).Return(nil)

	params := worker.BillingWorkflowParams{
		BillID:             "test-bill-id",
		AccountID:          "test-account-id",
		PeriodStart:        time.Now(),
		PeriodEnd:          time.Now().Add(24 * time.Hour),
		SettlementCurrency: "USD",
	}

	var closeErr error
	var addErr error

	// At 1 hour: Close the bill first
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow("CloseBill", "update-id-1", &testsuite.TestUpdateCallback{
			OnComplete: func(val interface{}, err error) {
				closeErr = err
			},
		})
	}, 1*time.Hour)

	// At 1 hour + 5 seconds: Verify close succeeded, then try to add line item to closed bill
	env.RegisterDelayedCallback(func() {
		assert.NoError(t, closeErr)

		env.UpdateWorkflow("AddLineItem", "update-id-2", &testsuite.TestUpdateCallback{
			OnComplete: func(val interface{}, err error) {
				addErr = err
			},
			OnReject: func(err error) {
				addErr = err
			},
		}, worker.AddLineItemInput{
			IdempotencyKey: "item-after-close",
			Description:    "item on closed bill",
			AmountMinor:    500,
			Currency:       "USD",
		})
	}, 1*time.Hour+5*time.Second)

	// At 1 hour + 10 seconds: Verify update rejected with "bill is closed"
	env.RegisterDelayedCallback(func() {
		assert.Error(t, addErr)
		assert.Contains(t, addErr.Error(), "bill is closed")
	}, 1*time.Hour+10*time.Second)

	env.ExecuteWorkflow(worker.BillingWorkflow, params)
	assert.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())
}

func TestCreateBill_UniqueOpenBillConstraint(t *testing.T) {
	ctx := context.Background()

	// Initialize Temporal client manually for integration test
	var err error
	tempClient, err := client.Dial(client.Options{
		HostPort: "localhost:7233",
	})
	assert.NoError(t, err)
	defer tempClient.Close()

	// Instantiate Service with manually created dependencies
	repo := repository.NewPostgresRepository(db)
	svc := &Service{
		client: tempClient,
		repo:   repo,
	}

	// Cleanup database before test
	_, err = db.Exec(ctx, "DELETE FROM line_items")
	assert.NoError(t, err)
	_, err = db.Exec(ctx, "DELETE FROM bill_totals")
	assert.NoError(t, err)
	_, err = db.Exec(ctx, "DELETE FROM bills")
	assert.NoError(t, err)

	accountID := "test-unique-open-bill-acc"
	now := time.Now().UTC().Truncate(time.Second)

	// 1. Create first OPEN bill
	resp1, err := svc.CreateBill(ctx, &CreateBillParams{
		AccountID:          accountID,
		PeriodStart:        now,
		PeriodEnd:          now.Add(24 * time.Hour),
		SettlementCurrency: "USD",
	})
	assert.NoError(t, err)
	assert.NotNil(t, resp1)
	assert.Equal(t, "OPEN", resp1.Status)

	// Clean up workflow when finished
	defer func() {
		_ = tempClient.TerminateWorkflow(ctx, resp1.WorkflowID, "", "test cleanup")
	}()

	// 2. Try to create second OPEN bill for same account with different period start
	resp2, err := svc.CreateBill(ctx, &CreateBillParams{
		AccountID:          accountID,
		PeriodStart:        now.Add(48 * time.Hour),
		PeriodEnd:          now.Add(72 * time.Hour),
		SettlementCurrency: "USD",
	})
	assert.Error(t, err)
	assert.Nil(t, resp2)
	assert.Contains(t, err.Error(), "account already has an active open bill")

	// 3. Try to create duplicate of first bill (idempotency check) - should succeed and return the first bill
	resp3, err := svc.CreateBill(ctx, &CreateBillParams{
		AccountID:          accountID,
		PeriodStart:        now,
		PeriodEnd:          now.Add(24 * time.Hour),
		SettlementCurrency: "USD",
	})
	assert.NoError(t, err)
	assert.NotNil(t, resp3)
	assert.Equal(t, resp1.ID, resp3.ID)
}

func TestBillingWorkflow_TerminationCycle(t *testing.T) {
	s := &testsuite.WorkflowTestSuite{}
	env := s.NewTestWorkflowEnvironment()

	// Capture activity call indicators
	closeBillCalledWithStatus := ""
	startNextCycleCalled := false

	var act *worker.Activities
	env.OnActivity(act.PersistLineItemActivity, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(act.CloseBillActivity, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		params := args.Get(1).(worker.CloseBillParams)
		closeBillCalledWithStatus = params.Status
	}).Return(nil)
	env.OnActivity(act.StartNextBillingCycleActivity, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		startNextCycleCalled = true
	}).Return(nil)

	params := worker.BillingWorkflowParams{
		BillID:             "test-term-bill-id",
		AccountID:          "test-account-id",
		PeriodStart:        time.Now(),
		PeriodEnd:          time.Now().Add(24 * time.Hour),
		SettlementCurrency: "USD",
	}

	var addResult worker.AddLineItemResult
	var addErr error
	var termResult worker.CloseBillResult
	var termErr error

	// At 1 hour: Add a line item
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow("AddLineItem", "update-id-1", &testsuite.TestUpdateCallback{
			OnComplete: func(val interface{}, err error) {
				if err == nil {
					addResult = val.(worker.AddLineItemResult)
				}
				addErr = err
			},
		}, worker.AddLineItemInput{
			IdempotencyKey: "item-1",
			Description:    "termination item",
			AmountMinor:    300,
			Currency:       "USD",
		})
	}, 1*time.Hour)

	// At 2 hours: Send TerminateBill update
	env.RegisterDelayedCallback(func() {
		assert.NoError(t, addErr)
		assert.Equal(t, int64(300), addResult.Totals["USD"])

		env.UpdateWorkflow("TerminateBill", "update-id-2", &testsuite.TestUpdateCallback{
			OnComplete: func(val interface{}, err error) {
				if err == nil {
					termResult = val.(worker.CloseBillResult)
				}
				termErr = err
			},
		})
	}, 2*time.Hour)

	env.ExecuteWorkflow(worker.BillingWorkflow, params)
	assert.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())

	// Assertions
	assert.NoError(t, termErr)
	assert.Equal(t, int64(300), termResult.Totals["USD"])
	assert.Equal(t, "TERMINATED", closeBillCalledWithStatus)
	assert.False(t, startNextCycleCalled)
}
