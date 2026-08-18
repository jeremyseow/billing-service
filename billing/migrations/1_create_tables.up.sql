CREATE TABLE bills (
    id VARCHAR(64) PRIMARY KEY,
    account_id VARCHAR(64) NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    status VARCHAR(16) DEFAULT 'OPEN' NOT NULL,
    settlement_currency VARCHAR(3) DEFAULT 'USD' NOT NULL,
    closed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE(account_id, period_start)
);

CREATE TABLE line_items (
    id VARCHAR(64) PRIMARY KEY,
    bill_id VARCHAR(64) NOT NULL REFERENCES bills(id),
    idempotency_key VARCHAR(128) NOT NULL,
    description TEXT NOT NULL,
    amount_minor BIGINT NOT NULL,
    currency VARCHAR(3) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE(bill_id, idempotency_key)
);

CREATE TABLE bill_totals (
    bill_id VARCHAR(64) NOT NULL REFERENCES bills(id),
    currency VARCHAR(3) NOT NULL,
    total_minor BIGINT NOT NULL,
    PRIMARY KEY(bill_id, currency)
);

CREATE UNIQUE INDEX idx_unique_open_bill_per_account 
ON bills (account_id) 
WHERE (status = 'OPEN');

