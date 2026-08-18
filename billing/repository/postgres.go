package repository

import (
	"context"
	"time"

	"billing-service/billing/domain"
	"encore.dev/storage/sqldb"
)

type PostgresRepository struct {
	db *sqldb.Database
}

func NewPostgresRepository(db *sqldb.Database) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// Compile-time check to ensure PostgresRepository implements domain.Repository
var _ domain.Repository = (*PostgresRepository)(nil)

func (r *PostgresRepository) InsertBill(ctx context.Context, bill *domain.Bill) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO bills (id, account_id, period_start, period_end, status, settlement_currency, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT (account_id, period_start)
		DO UPDATE SET updated_at = NOW()
		RETURNING id, account_id, period_start, period_end, status, settlement_currency, closed_at, created_at, updated_at
	`, bill.ID, bill.AccountID, bill.PeriodStart, bill.PeriodEnd, bill.Status, bill.SettlementCurrency).Scan(
		&bill.ID, &bill.AccountID, &bill.PeriodStart, &bill.PeriodEnd, &bill.Status, &bill.SettlementCurrency, &bill.ClosedAt, &bill.CreatedAt, &bill.UpdatedAt,
	)
}

func (r *PostgresRepository) GetBill(ctx context.Context, id string) (*domain.Bill, error) {
	var b domain.Bill
	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, period_start, period_end, status, settlement_currency, closed_at, created_at, updated_at
		FROM bills
		WHERE id = $1
	`, id).Scan(
		&b.ID, &b.AccountID, &b.PeriodStart, &b.PeriodEnd, &b.Status, &b.SettlementCurrency, &b.ClosedAt, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *PostgresRepository) GetBillSummary(ctx context.Context, id string) (*domain.BillSummary, error) {
	var summary domain.BillSummary

	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, period_start, period_end, status, settlement_currency, closed_at
		FROM bills
		WHERE id = $1
	`, id).Scan(
		&summary.ID, &summary.AccountID, &summary.PeriodStart, &summary.PeriodEnd, &summary.Status, &summary.SettlementCurrency, &summary.ClosedAt,
	)
	if err != nil {
		return nil, err
	}

	// Query totals
	rowsTotals, err := r.db.Query(ctx, `
		SELECT currency, total_minor
		FROM bill_totals
		WHERE bill_id = $1
	`, id)
	if err != nil {
		return nil, err
	}
	defer rowsTotals.Close()

	summary.Totals = []domain.TotalSummary{}
	for rowsTotals.Next() {
		var ts domain.TotalSummary
		if err := rowsTotals.Scan(&ts.Currency, &ts.TotalMinor); err != nil {
			return nil, err
		}
		summary.Totals = append(summary.Totals, ts)
	}

	// Query line items
	rowsItems, err := r.db.Query(ctx, `
		SELECT id, idempotency_key, description, amount_minor, currency, created_at
		FROM line_items
		WHERE bill_id = $1
		ORDER BY created_at ASC
	`, id)
	if err != nil {
		return nil, err
	}
	defer rowsItems.Close()

	summary.LineItems = []domain.ItemSummary{}
	for rowsItems.Next() {
		var item domain.ItemSummary
		if err := rowsItems.Scan(&item.ID, &item.IdempotencyKey, &item.Description, &item.AmountMinor, &item.Currency, &item.CreatedAt); err != nil {
			return nil, err
		}
		summary.LineItems = append(summary.LineItems, item)
	}

	return &summary, nil
}

func (r *PostgresRepository) SaveLineItem(ctx context.Context, item *domain.LineItem) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO line_items (id, bill_id, idempotency_key, description, amount_minor, currency, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (bill_id, idempotency_key) DO NOTHING
	`, item.ID, item.BillID, item.IdempotencyKey, item.Description, item.AmountMinor, item.Currency)
	return err
}

func (r *PostgresRepository) CloseBill(ctx context.Context, id string, status string, closedAt time.Time, totals map[string]int64) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(ctx, `
		UPDATE bills
		SET status = $1, closed_at = $2, updated_at = NOW()
		WHERE id = $3
	`, status, closedAt, id)
	if err != nil {
		return err
	}

	for currency, totalMinor := range totals {
		_, err = tx.Exec(ctx, `
			INSERT INTO bill_totals (bill_id, currency, total_minor)
			VALUES ($1, $2, $3)
			ON CONFLICT (bill_id, currency) DO UPDATE
			SET total_minor = EXCLUDED.total_minor
		`, id, currency, totalMinor)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *PostgresRepository) CheckActiveBillExists(ctx context.Context, accountID string, excludePeriodStart time.Time) (bool, error) {
	var activeExists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM bills 
			WHERE account_id = $1 AND status = 'OPEN' AND period_start != $2
		)
	`, accountID, excludePeriodStart).Scan(&activeExists)
	return activeExists, err
}
