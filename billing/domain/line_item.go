package domain

import "time"

type LineItem struct {
	ID             string
	BillID         string
	IdempotencyKey string
	Description    string
	AmountMinor    int64
	Currency       string
	CreatedAt      time.Time
}
