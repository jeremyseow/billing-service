## 1. Project Initialization and Schema Migration

- [x] 1.1 Create `encore.app` and `go.mod` at the workspace root to define the Encore project structure.
- [x] 1.2 Create the `billing/migrations` directory.
- [x] 1.3 Implement the PostgreSQL migration `1_create_tables.up.sql` defining `bills`, `line_items`, and `bill_totals` tables with `NUMERIC(20, 8)` columns, FX conversion metadata, and `idx_unique_open_bill_per_account` unique index.
- [x] 1.4 Create `docker-compose.yml` in the root directory to provision Temporal (using standard local dev images), Prometheus, and Grafana.
- [x] 1.5 Create Prometheus scrape configs (`prometheus.yml`) and basic Grafana datasource/dashboard configs inside a `.docker/` monitoring directory.

## 2. Temporal Workflow and Activity Definitions

- [x] 2.1 Implement `BillingWorkflow` state structure including in-memory tracking of status, `shopspring/decimal` `original_totals`, `settlement_total`, processed idempotency keys, and manual close/termination triggers.
- [x] 2.2 Implement `rates.ConvertRates` engine for FX rate conversion between USD and GEL.
- [x] 2.3 Implement the synchronous Temporal Update handler `AddLineItem` in `BillingWorkflow` with validations for status, currency, string decimal parsing, and FX conversion.
- [x] 2.4 Implement synchronous Temporal Update handlers `CloseBill` and `TerminateBill`.
- [x] 2.5 Implement `PersistLineItemActivity` to save line items with original/settlement fields to PostgreSQL with `ON CONFLICT DO NOTHING`.
- [x] 2.6 Implement `CloseBillActivity` to update bill status (`CLOSED` or `TERMINATED`), set `closed_at` timestamp, and snapshot final totals into `bill_totals`.
- [x] 2.7 Implement `StartNextBillingCycleActivity` to trigger the next billing cycle workflow for `bill:{account_id}:{next_period_start}` on non-terminated bills.
- [x] 2.8 Register `BillingWorkflow` and activities with Temporal worker in Encore service init hook.
- [x] 2.9 Instrument `BillingWorkflow` and activities with structured logging and Prometheus metrics.

## 3. Encore REST API Endpoints

- [x] 3.1 Implement `POST /bills` endpoint enforcing at-most-one open bill per account, inserting into PostgreSQL, and starting `BillingWorkflow`.
- [x] 3.2 Implement `POST /bills/:id/items` endpoint accepting string decimal amounts, dispatching `AddLineItem` Update, and returning updated totals.
- [x] 3.3 Implement `POST /bills/:id/close` and `POST /bills/:id/terminate` endpoints.
- [x] 3.4 Implement `GET /bills/:id` endpoint querying PostgreSQL to return bill metadata, line items with FX audit fields, original totals, and settlement grand total.

## 4. Verification and Automated Testing

- [x] 4.1 Write integration tests verifying that line items added to closed bills are rejected.
- [x] 4.2 Write unit/integration tests verifying idempotency key deduplication.
- [x] 4.3 Write tests verifying decimal precision, GEL-to-USD FX conversions, and mixed positive/negative multi-currency sums.
- [x] 4.4 Write tests verifying the unique open bill constraint per account and permanent termination workflow behavior.
