## Why

To introduce a robust, stateful Fees and Billing API service to handle account billing cycles, line item tracking, multi-currency aggregation with FX rate conversions, arbitrary-precision decimal representation, and automatic billing cycle transitions. Utilizing a Temporal workflow ensures deterministic, fault-tolerant lifecycle management of bills, while PostgreSQL provides structured queryable projections.

## What Changes

- **Billing Service**: Create a new Encore.dev Go service named `billing` to house the APIs, PostgreSQL migrations, FX rate conversion engine, and Temporal workflow and activity implementations.
- **Database Schema**: Implement PostgreSQL migrations defining the `bills`, `line_items`, and `bill_totals` tables with `NUMERIC(20, 8)` columns for exact decimal precision, multi-currency FX audit metadata, and a unique partial index ensuring at most one open bill per account.
- **Temporal Workflow**: Implement `BillingWorkflow` to manage the state machine for each bill, including in-memory state tracking for status, `shopspring/decimal` running totals per currency (`original_totals`), converted grand total (`settlement_total`), and processed line-item idempotency keys.
- **Temporal Update Handlers**:
  - `AddLineItem`: Synchronously validates status, accepts signed items, converts foreign currencies via `rates.ConvertRates`, triggers database persistence activity, and updates running totals.
  - `CloseBill`: Synchronously sets manual close trigger state and returns final totals.
  - `TerminateBill`: Synchronously finalizes current bill with status `TERMINATED` and halts workflow without spawning consecutive cycles.
- **REST API Endpoints**:
  - `POST /bills`: Initiates the workflow and records the bill (enforcing at most one open bill per account).
  - `POST /bills/:id/items`: Dispatches the `AddLineItem` Temporal Update (accepting string decimal amounts to prevent OpenAPI schema object validation errors).
  - `POST /bills/:id/close`: Dispatches the `CloseBill` Temporal Update.
  - `POST /bills/:id/terminate`: Dispatches the `TerminateBill` Temporal Update.
  - `GET /bills/:id`: Queries PostgreSQL directly to return the bill details, line items with FX metadata, original totals, and settlement grand total.
- **Observability**: Add structured logging (`encore.dev/log` and Temporal logger) and custom Prometheus metrics for billing states, tracking active workflows, line-item insertion rates, and error frequencies.
- **Local Development Environment**: Create a root-level `docker-compose.yml` to spin up a local Temporal server, Prometheus, and Grafana (for dashboards and metrics visualization).
- **Verification**: Add comprehensive unit and integration tests to verify workflow correctness, idempotency deduplication, FX conversion, termination, open bill constraints, and database projection reliability.

## Capabilities

### New Capabilities
- `fees-and-billing`: Defines the API schema, FX conversion engine, Temporal workflow orchestration, and database projections for bill lifecycles and multi-currency fee aggregation.

### Modified Capabilities
<!-- No modified capabilities -->

## Impact

- **New Service**: A new `billing` service folder in Encore containing APIs, FX conversion package (`rates/`), workflow registry, and database migrations.
- **Database**: Add `bills`, `line_items`, and `bill_totals` tables with decimal precision and unique open bill index to PostgreSQL.
- **External Dependencies**: Requires a running Temporal cluster for workflow orchestration and `github.com/shopspring/decimal` for arbitrary precision math.
- **Infrastructure Configuration**: Add a `docker-compose.yml` and configs for Prometheus (`prometheus.yml`) and Grafana inside a new `.docker/` monitoring directory at the root.
