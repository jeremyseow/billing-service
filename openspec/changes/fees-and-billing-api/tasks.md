## 1. Project Initialization and Schema Migration

- [x] 1.1 Create `encore.app` and `go.mod` at the workspace root to define the Encore project structure.
- [x] 1.2 Create the `billing/migrations` directory.
- [x] 1.3 Implement the PostgreSQL migration `1_create_tables.up.sql` defining `bills`, `line_items`, and `bill_totals` tables with the specified columns, foreign keys, and unique constraints.
- [x] 1.4 Create `docker-compose.yml` in the root directory to provision Temporal (using standard local dev images), Prometheus, and Grafana.
- [x] 1.5 Create Prometheus scrape configs (`prometheus.yml`) and basic Grafana datasource/dashboard configs inside a `.docker/` monitoring directory.

## 2. Temporal Workflow and Activity Definitions

- [x] 2.1 Implement `BillingWorkflow` state structure including in-memory tracking of status, currency bucket totals (`map[string]int64`), processed idempotency keys, and the manual close trigger.
- [x] 2.2 Implement the synchronous Temporal Update handler `AddLineItem` in `BillingWorkflow` with validations for status and currency.
- [x] 2.3 Implement the synchronous Temporal Update handler `CloseBill` to signal manual close trigger state.
- [x] 2.4 Implement `PersistLineItemActivity` to save line items to PostgreSQL with `ON CONFLICT DO NOTHING`.
- [x] 2.5 Implement `CloseBillActivity` to update bill status to `CLOSED`, set `closed_at` timestamp, and snapshot final totals into the `bill_totals` table.
- [x] 2.6 Implement `StartNextBillingCycleActivity` to trigger the next billing cycle workflow for `bill:{account_id}:{next_period_start}`.
- [x] 2.7 Register `BillingWorkflow` and its activities with a Temporal worker initialized inside the Encore service initialization hook.
- [x] 2.8 Instrument `BillingWorkflow` and activities with structured logging (using `workflow.GetLogger` and `encore.dev/log`).
- [x] 2.9 Define and export Prometheus metrics for tracking active workflows, line-item throughput, and error rates using Go's prometheus client.

## 3. Encore REST API Endpoints

- [x] 3.1 Implement `POST /bills` endpoint to insert the bill record into PostgreSQL and start the `BillingWorkflow` with its deterministic ID.
- [x] 3.2 Implement `POST /bills/:id/items` endpoint to lookup the bill, dispatch the `AddLineItem` Update, and return the running totals.
- [x] 3.3 Implement `POST /bills/:id/close` endpoint to lookup the bill, dispatch the `CloseBill` Update, and return the final totals.
- [x] 3.4 Implement `GET /bills/:id` endpoint querying PostgreSQL using a direct join across `bills`, `bill_totals`, and `line_items`.

## 4. Verification and Automated Testing

- [x] 4.1 Write integration tests verifying that line items added to closed bills are rejected with a 422 error.
- [x] 4.2 Write unit/integration tests verifying idempotency key deduplication.
- [x] 4.3 Write tests verifying mixed positive/negative multi-currency sum aggregates.
- [x] 4.4 Manually verify metrics export endpoint and Grafana dashboard connection by running the compose stack and executing a series of test requests.
