## Purpose

Provides a deterministic, stateful system to track bills, aggregate fees in multiple currencies, manage lifecycles, and support idempotent transaction operations.

## ADDED Requirements

### Requirement: Bill Lifecycle Initiation
The system SHALL support creating a bill for a given account and billing period (`period_start` to `period_end`) and starting its lifecycle tracking.

#### Scenario: Successful bill initiation
- **WHEN** a client requests creation of a bill for an account with a start and end timestamp
- **THEN** the system SHALL create the bill in `OPEN` status, register it in the database, and start a lifecycle workflow to manage it.

### Requirement: Adding Line Items with Idempotency and State Checks
The system SHALL support adding a line item to a bill. The system MUST reject adding line items if the bill status is not `OPEN`. The system MUST deduplicate requests using an `idempotency_key`.

#### Scenario: Adding item to an open bill
- **WHEN** a client adds a line item with a unique `idempotency_key` and a valid currency (USD or GEL) to an open bill
- **THEN** the system SHALL record the line item, update the running currency totals, and return the updated totals.

#### Scenario: Adding item to a closed bill
- **WHEN** a client tries to add a line item to a bill that is already closed
- **THEN** the system SHALL reject the request with an error indicating the bill is closed.

#### Scenario: Deduplicating line items with the same idempotency key
- **WHEN** a client submits a line item with an `idempotency_key` that has already been successfully processed for that bill
- **THEN** the system SHALL ignore the duplicate insert and return the existing running totals without adding the amount again.

#### Scenario: Invalid currency validation
- **WHEN** a client adds a line item with a currency other than USD or GEL
- **THEN** the system SHALL reject the request as invalid.

### Requirement: Multi-Currency Aggregation and Signed Amounts
The system SHALL maintain running and final totals aggregated by currency. The system MUST support positive amounts and negative credits, updating the totals accordingly.

#### Scenario: Running totals with mixed positive and negative items
- **WHEN** line items of positive and negative amounts in USD and GEL are added to an open bill
- **THEN** the system SHALL aggregate the values per currency (e.g., subtracting negative amounts) and report correct balances.

### Requirement: Manual Bill Closure
The system SHALL support manually triggering the closure of a bill before its scheduled period end.

#### Scenario: Manual closure request
- **WHEN** a client requests manual closure of an open bill
- **THEN** the system SHALL transition the bill status to `CLOSED`, record the closure timestamp, persist the final currency totals, and return the final totals.

### Requirement: Automatic Bill Rollover
The system SHALL automatically close the bill at the scheduled `period_end` if it is not already closed, record the final snapshot of totals, and automatically start the billing cycle for the next consecutive period.

#### Scenario: Automatic period rollover
- **WHEN** the current billing period reaches `period_end` and is not yet closed
- **THEN** the system SHALL transition the current bill status to `CLOSED`, snapshot its totals in the database, and automatically initiate the next billing cycle.

### Requirement: Direct Querying of Bill Details
The system SHALL support querying a bill's current details, including its metadata, running/final totals, and all associated line items.

#### Scenario: Retrieving detailed bill details
- **WHEN** a client queries a bill by its ID
- **THEN** the system SHALL query the databases directly and return the bill information, all line items, and the consolidated totals.

### Requirement: System Observability
The system SHALL output structured logs for all key billing lifecycle events and errors. The system MUST expose Prometheus metrics for tracking system health, running workflow counts, line-item insertion rates, and processed amounts.

#### Scenario: Observability telemetry generation
- **WHEN** billing events (such as workflow start, line item insert, manual close, or rollover) are executed
- **THEN** structured log messages containing context fields (`account_id`, `bill_id`, `workflow_id`) SHALL be printed, and corresponding Prometheus metric counters and gauges SHALL be incremented.

### Requirement: At Most One Active Open Bill Constraint
The system SHALL enforce that an account can have at most one active open bill at any time. This constraint MUST be validated at the API layer and enforced at the database layer.

#### Scenario: Attempting to create an open bill when one already exists
- **WHEN** a client attempts to create an open bill for an account that already has a bill in `OPEN` status
- **THEN** the system SHALL reject the request with an error indicating an active bill already exists.

### Requirement: Permanent Billing Termination
The system SHALL support terminating a billing period permanently. This operation MUST finalize the current bill with a `TERMINATED` status, record the accumulated totals, and prevent the auto-spawning of any consecutive billing cycle.

#### Scenario: Terminating a bill
- **WHEN** a client requests permanent termination of an open bill
- **THEN** the system SHALL finalize the bill with status `TERMINATED`, persist the accumulated totals, and stop the workflow without rolling over to the next cycle.
