## Context

See [proposal.md](file:///G:/Users/Jeremy%20Seow/Side%20Projects/pave-bank/openspec/changes/fees-and-billing-api/proposal.md) for background and motivation. This document covers the technical details of the Billing service implementation in Encore.dev using Go, PostgreSQL, and Temporal workflows.

## Goals / Non-Goals

**Goals:**
- Implement a stateful Encore.dev service with PostgreSQL projections for query performance.
- Use a Temporal workflow (`BillingWorkflow`) to manage the lifecycle of bills, support synchronous line-item updates, and handle automatic cycle rollover.
- Standardize multi-currency aggregation and ensure idempotent insertions using line-item unique keys.
- Expose REST API endpoints to manage bills.
- Set up a complete local development environment using Docker Compose with Temporal, Prometheus, and Grafana.
- Implement structured logging and export Prometheus metrics for real-time monitoring and visualization.

**Non-Goals:**
- Integration with payment gateways or settlement execution networks.
- Implementing billing notifications (e.g., emailing invoices).
- Multi-currency conversion/exchange rate logic (currencies are aggregated in separate buckets).

## Decisions

### 1. Deterministic Workflow ID
- **Choice**: The Workflow ID for the `BillingWorkflow` will be formatted as `bill:{account_id}:{period_start_rfc3339}` where `period_start_rfc3339` is formatted as RFC3339 (e.g. `2026-08-16T00:00:00Z`).
- **Rationale**: Prevents multiple concurrent workflows from running for the same account's billing period. It makes it trivial to route requests (like adding items or closing) to the correct workflow instance using only the database record's account ID and start period.
- **Alternatives Considered**: Random UUIDs stored in the database. While standard, it requires database lookups for every routing action and doesn't provide built-in concurrency guarantees at the workflow engine layer.

### 2. State Partitioning between Temporal and PostgreSQL
- **Choice**:
  - **Temporal**: Primary source of truth for runtime execution, running totals, and idempotency tracking of pending line items.
  - **PostgreSQL**: Used as a read projection. Line items are written to PostgreSQL via `PersistLineItemActivity` within the `AddLineItem` update handler. Upon closure, final bill status and total summaries are written via `CloseBillActivity`.
- **Rationale**: Keeps database query performance high (`GET /bills/:id` can read directly from Postgres without querying Temporal history), while maintaining Temporal's guarantees of memory consistency, reliability, and correctness during active billing cycles.

### 3. Synchronous Update Handlers
- **Choice**: Use Temporal's synchronous Update Handlers (`AddLineItem` and `CloseBill`).
- **Rationale**: Enables callers to get immediate confirmation (success, running totals, or rejection/closed validation errors) without polling or complex callback loops.

### 4. Automatic Rollover Execution
- **Choice**: Execute next cycle start within the workflow completion sequence.
  - After transitioning to `CLOSED` and finishing `CloseBillActivity`, the current workflow starts the next billing cycle workflow `bill:{account_id}:{next_period_start}` using an activity `StartNextBillingCycleActivity`.
- **Rationale**: Guarantees a continuous sequence of billing cycles without requiring external cron jobs or scheduler setups. Doing this in an activity rather than a child workflow avoids parent-child dependency complexity.

### 5. Local Environment Orchestration
- **Choice**: Use a root `docker-compose.yml` to orchestrate Temporal, Prometheus, and Grafana, while letting Encore manage the PostgreSQL database instance.
- **Rationale**: Encore natively provisions and manages Go dependencies and PostgreSQL databases for local development. By omitting the database from the docker-compose file and relying on Encore's database management, we avoid port conflicts and integration complexity, while still spinning up Temporal, Prometheus, and Grafana in Docker.

### 6. Observability and Telemetry
- **Choice**: Use Encore's structured logging framework (`encore.dev/log`) and Temporal's `workflow.GetLogger` to output logs. Expose a Prometheus metrics endpoint to export custom counters (e.g. `billing_line_items_total`, `billing_failures_total`) and gauges (e.g. `active_billing_workflows`).
- **Rationale**: Standardizes log formats for searchability in dashboards and exports key indicators to Prometheus to visualize system status in Grafana.

## Risks / Trade-offs

- **Risk**: Database write failure during activity execution.
  - **Mitigation**: Temporal activities will automatically retry on transient errors. `PersistLineItemActivity` is designed to be idempotent using PostgreSQL's `ON CONFLICT DO NOTHING` on `(bill_id, idempotency_key)`.
- **Risk**: Temporal update handler latency.
  - **Mitigation**: Update handlers execute in memory and wait for `PersistLineItemActivity` completion. Since database writes are fast single-row inserts, latency will remain within acceptable limits (< 100ms).
