# Fees and Billing Service

A stateful, multi-currency Fees and Billing API service built with Go using the **Encore.dev** framework, orchestrated by **Temporal** workflows for lifecycle management, and backed by **PostgreSQL** read projections.

## Architecture Overview

1. **Encore REST APIs**: Entrypoints for initiating bills, adding line items (with idempotency), triggering manual closures, and querying consolidated summaries.
2. **Temporal Workflow (`BillingWorkflow`)**: Handles the stateful lifecycle of each billing period deterministically, manages running currency totals, filters duplicate line items, and rolls over to the next consecutive period.
3. **Database Projections**: Direct transaction writes for line items and snapshot summaries are stored in PostgreSQL (`bills`, `line_items`, and `bill_totals` tables).
4. **Observability Stack**: Local container orchestration for Temporal, Prometheus (scraping metrics), and Grafana (pre-configured dashboards for active cycles and throughput).

---

## Project Structure (Clean Architecture)

The codebase is structured to separate delivery, business logic, persistence, and workflow orchestrations cleanly

```
billing/
├── domain/            # Domain Layer: Core models & validation (no dependencies)
│   ├── bill.go        
│   ├── line_item.go   
│   └── repository.go  # Repository interface definition
├── rates/             # FX Engine: Rate conversion logic & currency validation
│   └── converter.go   
├── repository/        # Concrete SQL queries & transactions implementing domain.Repository
│   └── postgres.go     
├── worker/            # Orchestration Layer: Temporal state machines
│   ├── workflow.go    
│   └── activities.go  
├── service.go         # Service init: Constructor DI & Temporal worker registration
├── api.go             # Delivery Layer: Encore REST API handlers (Service method bindings)
├── migrations/        # SQL Migration scripts
│   └── 1_create_tables.up.sql
└── billing_test.go    # Mocks, unit, and integration tests
```

---

## Considerations and Trade-offs

### 1. Money Representation & Precision (`shopspring/decimal`)
To prevent floating-point rounding issues common with native float types (e.g. `0.1 + 0.2 != 0.3`) and avoid integer truncation errors during multi-currency FX conversions, all monetary values use arbitrary-precision decimals via the **`github.com/shopspring/decimal`** package.

In PostgreSQL, amounts are stored as **`NUMERIC(20, 8)`** columns (`line_items.original_amount`, `line_items.fx_rate`, `line_items.settlement_amount`, and `bill_totals.total`). This guarantees:
- Exact decimal math without floating-point representation drift.
- Support for micro-metered SaaS pricing (e.g., `$0.000015` per API token).
- Seamless JSON serialization and native database scanning.

### 2. Temporal's Role (Solving Distributed Systems Problems)
* **Race Condition Prevention**: Temporal processes all API updates for a specific workflow (bill ID) sequentially. By handling `AddLineItem` through a synchronous workflow update handler, concurrent request race conditions are mitigated automatically.
* **Reliable Long-Running Timers**: Billing cycles last for periods (e.g., weeks or months). Temporal handles these sleep durations natively using workflow timers, even if the application servers restart, crash, or redeploy in the middle of a billing period.
* **Fault Tolerance & Retries**: Activities interacting with external systems or databases (e.g., closing bills) are configured with Temporal retry policies to recover from transient failures automatically.

### 3. API Semantics (RESTful Financial API Patterns)
* **Strict Idempotency**: Financial writes (adding line items) require an `idempotency_key` parameter. The workflow tracks processed keys in-memory (`state.ProcessedIdempotency`) to return cached totals immediately, while the PostgreSQL database enforces a composite unique constraint (`UNIQUE(bill_id, idempotency_key)`) as a storage-level safeguard.
* **RESTful State Modifiers**: Operations that modify state are modeled as standard POST endpoints:
  * `POST /bills` (Initiates/registers a bill)
  * `POST /bills/:id/items` (Appends line item)
  * `POST /bills/:id/close` (Finalizes current cycle)
  * `POST /bills/:id/terminate` (Terminates billing permanently)
* **Deterministic Errors**: Clear HTTP status mapping via Encore's `errs` (e.g., `404 Not Found` for invalid IDs, `409 Already Exists` for constraint violations).

### 4. Data Modeling & State Machine
A bill follows a sequential state machine: `OPEN` ➡️ `CLOSED` or `TERMINATED`.
* **Database Constraint**: We enforce a database-level unique constraint (`idx_unique_open_bill_per_account`) on `bills` checking `account_id` where `status = 'OPEN'`. This guarantees at most one open bill per account at any time.
* **Termination (Halting Rollovers)**: A standard manual closure (`/close`) finalizes totals and rolls over to register the next consecutive billing period. A termination (`/terminate`) finalizes the bill as `TERMINATED` and halts the Temporal workflow immediately, skipping rollover logic.

---

## How to Run

### 1. Start Orchestration Stack
Launch Temporal, Prometheus, and Grafana:
```bash
docker compose up -d
```
* **Temporal Web UI**: [http://localhost:8233](http://localhost:8233)
* **Grafana**: [http://localhost:3000](http://localhost:3000) (`admin` / `admin`)

### 2. Run the Service
Start the Encore development daemon (auto-provisions local Postgres and runs migrations):
```bash
encore run
```

---

## How to Test

### Running Automated Tests
Run Go unit and integration tests (uses simulated Temporal clocks and memory Postgres sandboxes):
```bash
encore test -v ./billing
```

### Manual Testing with Curl

#### 1. Create a Bill (`POST /bills`)
* **What it does**: Initiates a new billing period workflow for a specific account. It enforces that at most one bill can be `OPEN` for an account at any time.
* **Curl Command**:
  ```bash
  curl http://localhost:4000/bills -X POST -H "Content-Type: application/json" \
    -d '{"account_id": "acc-123", "period_start": "2026-08-18T00:00:00Z", "period_end": "2026-08-25T00:00:00Z", "settlement_currency": "USD"}'
  ```
* **Expected Result**: Returns the initialized bill details with status `"OPEN"` and a generated UUID `id` (which also acts as the Temporal `workflow_id`).
  ```json
  {
    "id": "c04c3185-4e7d-4fa1-85da-ecadffb10fcb",
    "account_id": "acc-123",
    "workflow_id": "c04c3185-4e7d-4fa1-85da-ecadffb10fcb",
    "period_start": "2026-08-18T00:00:00Z",
    "period_end": "2026-08-25T00:00:00Z",
    "status": "OPEN",
    "settlement_currency": "USD",
    "closed_at": null,
    "created_at": "2026-08-18T22:25:00Z",
    "updated_at": "2026-08-18T22:25:00Z"
  }
  ```

#### 2. Add a Line Item (`POST /bills/:id/items`)
* **What it does**: Appends a charge or credit to an active bill. Validates that the currency is USD or GEL, converts foreign currencies using exchange rates, rejects items if the bill is closed, and deduplicates identical requests using the `idempotency_key`.
* **Curl Command** (Replace `<id>` with the bill UUID):
  ```bash
  curl http://localhost:4000/bills/<id>/items -X POST -H "Content-Type: application/json" \
    -d '{"idempotency_key": "tx-1", "description": "Transaction Fee", "amount": "150.50", "currency": "USD"}'
  ```
* **Expected Result**: Returns the updated original currency breakdown and running grand total in settlement currency:
  ```json
  {
    "original_totals": {
      "USD": "150.5"
    },
    "settlement_total": "150.5"
  }
  ```

#### 3. Manually Close a Bill (`POST /bills/:id/close`)
* **What it does**: Closes the active billing period early. Persists the final totals in the database, transitions the status to `CLOSED`, and automatically kicks off the billing workflow and Postgres row for the next consecutive cycle.
* **Curl Command** (Replace `<id>` with the bill UUID):
  ```bash
  curl http://localhost:4000/bills/<id>/close -X POST
  ```
* **Expected Result**: Returns the final finalized totals snapshot:
  ```json
  {
    "original_totals": {
      "USD": "150.5"
    },
    "settlement_total": "150.5"
  }
  ```

#### 4. Terminate a Bill (`POST /bills/:id/terminate`)
* **What it does**: Terminates the billing cycle permanently. Finalizes the current bill under the status `TERMINATED` and blocks any future rollover periods from spawning.
* **Curl Command** (Replace `<id>` with the bill UUID):
  ```bash
  curl http://localhost:4000/bills/<id>/terminate -X POST
  ```
* **Expected Result**: Returns the final totals recorded upon termination:
  ```json
  {
    "original_totals": {
      "USD": "150.5"
    },
    "settlement_total": "150.5"
  }
  ```

#### 5. Retrieve Bill Details (`GET /bills/:id`)
* **What it does**: Directly queries the PostgreSQL database (joining bills, totals, and line items) to construct a complete summary.
* **Curl Command** (Replace `<id>` with the bill UUID):
  ```bash
  curl http://localhost:4000/bills/<id>
  ```
* **Expected Result**: Returns a comprehensive JSON payload of the bill state, original totals breakdown, settlement grand total, and all associated line items with FX rate audit metadata:
  ```json
  {
    "id": "c04c3185-4e7d-4fa1-85da-ecadffb10fcb",
    "account_id": "acc-123",
    "workflow_id": "c04c3185-4e7d-4fa1-85da-ecadffb10fcb",
    "period_start": "2026-08-18T00:00:00Z",
    "period_end": "2026-08-25T00:00:00Z",
    "status": "OPEN",
    "settlement_currency": "USD",
    "settlement_total": "150.5",
    "original_totals": [
      {
        "currency": "USD",
        "total": "150.5"
      }
    ],
    "line_items": [
      {
        "id": "c04c3185-4e7d-4fa1-85da-ecadffb10fcb:tx-1",
        "idempotency_key": "tx-1",
        "description": "Transaction Fee",
        "original_amount": "150.5",
        "original_currency": "USD",
        "fx_rate": "1",
        "settlement_amount": "150.5",
        "settlement_currency": "USD",
        "created_at": "2026-08-18T22:26:00Z"
      }
    ],
    "closed_at": null
  }
  ```
