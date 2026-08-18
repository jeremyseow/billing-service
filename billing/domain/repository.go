package domain

import (
	"context"
	"time"
)

type Repository interface {
	InsertBill(ctx context.Context, bill *Bill) error
	GetBill(ctx context.Context, id string) (*Bill, error)
	GetBillSummary(ctx context.Context, id string) (*BillSummary, error)
	SaveLineItem(ctx context.Context, item *LineItem) error
	CloseBill(ctx context.Context, id string, status string, closedAt time.Time, totals map[string]int64) error
	CheckActiveBillExists(ctx context.Context, accountID string, excludePeriodStart time.Time) (bool, error)
}
