package domain

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
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
	Currency string          `json:"currency"`
	Total    decimal.Decimal `json:"total"`
}

type ItemSummary struct {
	ID                 string          `json:"id"`
	IdempotencyKey     string          `json:"idempotency_key"`
	Description        string          `json:"description"`
	OriginalAmount     decimal.Decimal `json:"original_amount"`
	OriginalCurrency   string          `json:"original_currency"`
	FXRate             decimal.Decimal `json:"fx_rate"`
	SettlementAmount   decimal.Decimal `json:"settlement_amount"`
	SettlementCurrency string          `json:"settlement_currency"`
	CreatedAt          time.Time       `json:"created_at"`
}

type BillSummary struct {
	ID                 string          `json:"id"`
	AccountID          string          `json:"account_id"`
	WorkflowID         string          `json:"workflow_id"`
	PeriodStart        time.Time       `json:"period_start"`
	PeriodEnd          time.Time       `json:"period_end"`
	Status             string          `json:"status"`
	SettlementCurrency string          `json:"settlement_currency"`
	SettlementTotal    decimal.Decimal `json:"settlement_total"`
	OriginalTotals     []TotalSummary  `json:"original_totals"`
	LineItems          []ItemSummary   `json:"line_items"`
	ClosedAt           *time.Time      `json:"closed_at"`
}

func (b *Bill) Validate() error {
	if b.SettlementCurrency != "USD" && b.SettlementCurrency != "GEL" {
		return errors.New("invalid settlement currency")
	}
	return nil
}
