## Context

See [proposal.md] for background and motivation. This document covers the technical details of the Billing service implementation in Encore.dev using Go, PostgreSQL, and Temporal workflows.

## Goals / Non-Goals

**Goals:**
- Implement a stateful Encore.dev service with PostgreSQL projections for query performance.
- Use a Temporal workflow (`BillingWorkflow`) to manage the lifecycle of bills, support synchronous line-item updates, and handle automatic cycle rollover.
- Standardize multi-currency aggregation with in-memory FX rate conversions using `shopspring/decimal` for exact precision math.
- Support full audit logging of line items, storing original currency & amount, FX conversion rate, and settlement currency & amount.
- Support permanent billing termination (`POST /bills/:id/terminate`).
- Expose REST API endpoints to manage bills and enforce at most one open bill per account.
- Set up a complete local development environment using Docker Compose with Temporal, Prometheus, and Grafana.
- Implement structured logging and export Prometheus metrics for real-time monitoring and visualization.

**Non-Goals:**
- Integration with external live payment gateways or banking settlement networks.
- Implementing billing notifications (e.g., emailing invoices).

## Decisions

### 1. Money Representation & Precision (`shopspring/decimal`)
- **Choice**: All monetary amounts are represented using `shopspring/decimal` in Go and PostgreSQL `NUMERIC(20, 8)` columns.
- **Rationale**: Prevents floating-point rounding errors (e.g. `0.1 + 0.2 != 0.3`) and avoids integer truncation during currency FX conversions while supporting micro-metered pricing.
- **API Param Strategy**: Request payload DTOs accept string decimal representations (e.g. `"150.50"` parsed via `decimal.NewFromString`) to prevent OpenAPI schema generator object validation issues in Encore, while response payloads marshal `decimal.Decimal` natively.

### 2. Multi-Currency & FX Rate Conversion Audit Trail
- **Choice**: Store both original transaction data and settlement conversion data for every line item (`original_amount`, `original_currency`, `fx_rate`, `settlement_amount`, `settlement_currency`).
- **Rationale**: Provides clear financial auditing. `rates.ConvertRates` converts foreign currencies (e.g., GEL to USD at rate `0.353979238755`) deterministically before persistence, while `original_totals` maintains running balances per original currency.

### 3. Deterministic Workflow ID
- **Choice**: The Workflow ID for the `BillingWorkflow` will be formatted as `bill:{account_id}:{period_start_rfc3339}` where `period_start_rfc3339` is formatted as RFC3339 (e.g. `2026-08-16T00:00:00Z`).
- **Rationale**: Prevents multiple concurrent workflows from running for the same account's billing period. It makes it trivial to route requests (like adding items or closing) to the correct workflow instance using only the database record's account ID and start period.

### 4. State Partitioning between Temporal and PostgreSQL
- **Choice**:
  - **Temporal**: Primary source of truth for runtime execution, running currency totals (`original_totals`), converted total (`settlement_total`), and idempotency tracking of pending line items.
  - **PostgreSQL**: Used as a read projection. Line items are written to PostgreSQL via `PersistLineItemActivity` within the `AddLineItem` update handler. Upon closure, final bill status and total summaries are written via `CloseBillActivity`.
- **Rationale**: Keeps database query performance high (`GET /bills/:id` can read directly from Postgres without querying Temporal history), while maintaining Temporal's guarantees of memory consistency, reliability, and correctness during active billing cycles.

### 5. Synchronous Update Handlers
- **Choice**: Use Temporal's synchronous Update Handlers (`AddLineItem`, `CloseBill`, and `TerminateBill`).
- **Rationale**: Enables callers to get immediate confirmation (success, running totals, or rejection/closed validation errors) without polling or complex callback loops.

### 6. Permanent Billing Termination
- **Choice**: Expose `POST /bills/:id/terminate` which dispatches `TerminateBill` update handler to set status to `TERMINATED` and complete the workflow without calling `StartNextBillingCycleActivity`.
- **Rationale**: Allows accounts to offboard or permanently end billing cycles without forcing consecutive period rollovers.

### 7. At Most One Open Bill Per Account
- **Choice**: Enforce a partial unique index in PostgreSQL (`idx_unique_open_bill_per_account ON bills (account_id) WHERE status = 'OPEN'`) alongside API-level checks.
- **Rationale**: Storage-level guarantee ensuring an account can never enter an inconsistent state with multiple active billing cycles running simultaneously.

### 8. Local Environment Orchestration
- **Choice**: Use a root `docker-compose.yml` to orchestrate Temporal, Prometheus, and Grafana, while letting Encore manage the PostgreSQL database instance.
- **Rationale**: Encore natively provisions and manages Go dependencies and PostgreSQL databases for local development. By omitting the database from the docker-compose file and relying on Encore's database management, we avoid port conflicts and integration complexity.

### 9. Observability and Telemetry
- **Choice**: Use Encore's structured logging framework (`encore.dev/log`) and Temporal's `workflow.GetLogger` to output logs. Expose a Prometheus metrics endpoint to export custom counters (e.g. `billing_line_items_total`, `billing_failures_total`) and gauges (e.g. `active_billing_workflows`).
- **Rationale**: Standardizes log formats for searchability in dashboards and exports key indicators to Prometheus to visualize system status in Grafana.
