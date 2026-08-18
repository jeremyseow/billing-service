## Why

To introduce a robust, stateful Fees and Billing API service to handle account billing cycles, line item tracking, multi-currency aggregation, and automatic billing cycle transitions. Utilizing a Temporal workflow ensures deterministic, fault-tolerant lifecycle management of bills, while PostgreSQL provides structured queryable projections.

## What Changes

- **Billing Service**: Create a new Encore.dev Go service named `billing` to house the APIs, PostgreSQL migration, and Temporal workflow and activity implementations.
- **Database Schema**: Implement a new PostgreSQL migration defining the `bills`, `line_items`, and `bill_totals` tables.
- **Temporal Workflow**: Implement `BillingWorkflow` to manage the state machine for each bill, including in-memory state tracking for status, currency bucket totals, and processed line-item idempotency keys.
- **Temporal Update Handlers**:
  - `AddLineItem`: Synchronously validates status, accepts signed items, triggers database persistence activity, and updates bucket totals.
  - `CloseBill`: Synchronously sets manual close trigger state.
- **REST API Endpoints**:
  - `POST /bills`: Initiates the workflow and records the bill.
  - `POST /bills/:id/items`: Dispatches the `AddLineItem` Temporal Update.
  - `POST /bills/:id/close`: Dispatches the `CloseBill` Temporal Update.
  - `GET /bills/:id`: Queries PostgreSQL directly to return the bill details, line items, and totals.
- **Observability**: Add structured logging (`encore.dev/log` and Temporal logger) and custom Prometheus metrics for billing states, tracking active workflows, line-item insertion rates, and error frequencies.
- **Local Development Environment**: Create a root-level `docker-compose.yml` to spin up a local Temporal server, Prometheus, and Grafana (for dashboards and metrics visualization). Note that Encore will continue to manage the development PostgreSQL instance.
- **Verification**: Add comprehensive unit and integration tests to verify workflow correctness, idempotency deduplication, and database projection reliability.

## Capabilities

### New Capabilities
- `fees-and-billing`: Defines the API schema, Temporal workflow orchestration, and database projections for bill lifecycles and fee aggregation.

### Modified Capabilities
<!-- No modified capabilities -->

## Impact

- **New Service**: A new `billing` service folder in Encore containing APIs, workflow registry, and database migrations.
- **Database**: Add `bills`, `line_items`, and `bill_totals` tables to the application's PostgreSQL database.
- **External Dependencies**: Requires a running Temporal cluster for workflow orchestration.
- **Infrastructure Configuration**: Add a `docker-compose.yml` and configs for Prometheus (`prometheus.yml`) and Grafana inside a new `.docker/` monitoring directory at the root.
