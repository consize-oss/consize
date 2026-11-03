# Recommendation and Action Contracts

## Purpose

Consize stores a recommendation and an action as different records.

- A **recommendation** is an immutable proposal produced by a named algorithm from identified evidence for one resource.
- An **action** is one governed attempt to realize that proposal through a selected remediation path and plugin.
- An **action event** is an append-only audit entry describing something that happened to an action. It is not the action itself.
- A **job** is worker state used to resume an approved direct-apply action after a restart.

This separation keeps the original decision explainable while allowing an action to be planned, retried, verified, rolled back, or failed without rewriting history.

## Identity and Versioning

Both records have stable numeric IDs and `schema_version`. An action must reference an existing recommendation. The recommendation and action must name the same resource, plugin, and action type.

A recommendation records:

- resource identity;
- recommendation type and proposed change;
- rationale and human-readable evidence;
- durable evidence references;
- algorithm identity and version;
- confidence and risk;
- an explicitly classified savings estimate;
- creation, update, and optional expiry times;
- lifecycle status and supersession linkage.

An action records:

- originating recommendation and resource;
- remediation path, action plugin, plugin version, and mode;
- caller-supplied or deterministic idempotency key;
- requester and approver;
- captured policy decision;
- plan result separately from execution result;
- failure code and message;
- lifecycle timestamps.

Fields describing why a recommendation was created are immutable. New evidence creates a new recommendation; it does not rewrite the old one. Action status, results, and lifecycle timestamps may change only through store transition methods.

## Recommendation Lifecycle

```text
pending -> planned -> approved -> executing -> verified
   |          |          |            |          
   |          |          |            +-> failed
   |          |          |            +-> rolled_back
   |          |          +-> rejected
   |          +-> rejected | expired | superseded
   +-> rejected | expired | superseded
```

Terminal recommendation states are `verified`, `rejected`, `expired`, `superseded`, `failed`, `rolled_back`, and `manual_intervention`. A superseded record points to its replacement. Expired and superseded recommendations cannot create new actions.

Resource or evidence changes do not mutate the proposal. The analysis path creates a replacement recommendation and explicitly supersedes the earlier one after both records exist. This preserves auditability and prevents an approval for one proposal from silently authorizing a different proposal.

## Action Lifecycle

```text
requested -> planning -> planned -> approved -> executing -> verifying -> succeeded
     |           |          |          |           |            |
     +-----------+----------+----------+-----------+------------+-> failed
                 |          |          |           +-> rollback_pending
                 +----------+----------+------------------------> cancelled

failed | executing | verifying -> rollback_pending -> rolling_back -> rolled_back
                                  |                    |
                                  +--------------------+-> manual_intervention
```

Terminal action states are `succeeded`, `rolled_back`, `manual_intervention`, and `cancelled`. `failed` is not terminal because policy may permit a captured-state rollback.

The store rejects transitions not present in the transition table. Callers cannot move a completed action back to execution or mark a requested action successful without the intermediate governed stages.

## Planning and Execution

A plan or dry run is not proof that infrastructure changed. It is stored in `plan_result` with its own creation time. Live plugin output is stored separately in `execution_result`, with execution timestamps. The audit stream can describe both without conflating them.

`dry_run` actions stop after planning. `approved` actions may enter the durable worker lifecycle only after policy, authorization, preflight, baseline health, verification configuration, and rollback capability checks pass.

## Idempotency

The idempotency key identifies one action request. Reusing the same key returns the original action instead of creating another action. The API accepts `Idempotency-Key`; internal callers use a deterministic key when a caller does not provide one.

An idempotency key is immutable and globally unique in the current OSS store. It does not authorize execution and cannot bypass recommendation status, policy, preflight, or plugin checks.

## Savings Outcomes

Savings use explicit classifications:

- `estimated`: rate-card or model-based projection;
- `operationally_verified`: the change passed configured health checks, but billing is not yet proven;
- `financially_realized`: billing evidence later confirms a financial outcome.

Operational verification must never silently promote an estimate to realized billing savings.

## API and Audit

- `GET /api/action-records` returns durable action records.
- `GET /api/actions` remains the append-only audit-event feed for compatibility.
- `/api/dashboard` exposes both `action_records` and the existing `actions` audit feed.
- Recommendation plan and execute endpoints create or reuse an action through an idempotency key.

Audit entries reference the recommendation and describe policy, plan, worker, verification, and recovery events. The durable action is the current governed state; audit events are the historical account of how it reached that state.

## Compatibility

Durable state schema v4 introduces an independent action collection and event sequence. A v3 state file migrates without reinterpreting old audit events as complete actions. Existing events remain available, while new requests use the explicit action contract.

This avoids inventing approval, idempotency, or execution data that old events never captured.

## Allowed transitions

The diagrams above show common paths. The complete accepted transitions are
listed below; an omitted transition, including a transition to the same state,
is rejected by [the store contract](https://github.com/consize-oss/consize/blob/main/internal/store/contracts.go).

Recommendations:

- `pending` → `planned`, `approved`, `rejected`, `expired`, `superseded`.
- `planned` → `approved`, `rejected`, `expired`, `superseded`.
- `approved` → `executing`, `rejected`, `expired`, `superseded`.
- `executing` → `verified`, `failed`, `rolled_back`, `manual_intervention`.

Actions:

- `requested` → `planning`, `planned`, `approved`, `failed`, `cancelled`.
- `planning` → `planned`, `failed`, `cancelled`.
- `planned` → `approved`, `cancelled`, `failed`.
- `approved` → `executing`, `cancelled`, `failed`.
- `executing` → `verifying`, `succeeded`, `failed`, `rollback_pending`, `manual_intervention`.
- `verifying` → `succeeded`, `failed`, `rollback_pending`, `manual_intervention`.
- `failed` → `rollback_pending`, `manual_intervention`.
- `rollback_pending` → `rolling_back`, `manual_intervention`.
- `rolling_back` → `rolled_back`, `manual_intervention`.

These are model transition rules, not permission to bypass policy or preflight.
The worker owns a separate recovery state machine. See
[core model contracts](../reference/core-model.md) for field ownership and
[transition tests](https://github.com/consize-oss/consize/blob/main/internal/store/contracts_test.go) for enforcement.
