package domain

import (
	"errors"
	"time"
)

type Bill struct {
	ID                 string
	AccountID          string
	PeriodStart        time.Time
	PeriodEnd          time.Time
	Status             string
	SettlementCurrency string
	ClosedAt           *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type TotalSummary struct {
	Currency   string `json:"currency"`
	TotalMinor int64  `json:"total_minor"`
}

type ItemSummary struct {
	ID             string    `json:"id"`
	IdempotencyKey string    `json:"idempotency_key"`
	Description    string    `json:"description"`
	AmountMinor    int64     `json:"amount_minor"`
	Currency       string    `json:"currency"`
	CreatedAt      time.Time `json:"created_at"`
}

type BillSummary struct {
	ID                 string         `json:"id"`
	AccountID          string         `json:"account_id"`
	WorkflowID         string         `json:"workflow_id"`
	PeriodStart        time.Time      `json:"period_start"`
	PeriodEnd          time.Time      `json:"period_end"`
	Status             string         `json:"status"`
	SettlementCurrency string         `json:"settlement_currency"`
	ClosedAt           *time.Time     `json:"closed_at"`
	Totals             []TotalSummary `json:"totals"`
	LineItems          []ItemSummary  `json:"line_items"`
}

func (b *Bill) Validate() error {
	if b.SettlementCurrency != "USD" && b.SettlementCurrency != "GEL" {
		return errors.New("invalid settlement currency")
	}
	return nil
}
