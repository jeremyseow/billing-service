package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

type LineItem struct {
	ID                 string
	BillID             string
	IdempotencyKey     string
	Description        string
	OriginalAmount     decimal.Decimal
	OriginalCurrency   string
	FXRate             decimal.Decimal
	SettlementAmount   decimal.Decimal
	SettlementCurrency string
	CreatedAt          time.Time
}
