# ADR 0002: Preserve evidence and separate proposals from attempts

- Status: Accepted (documents the existing implementation)
- Date: 2026-11-03

## Context

Fresh discovery and measurements can change the best proposal. Retries, failures
and rollbacks can change an attempt's outcome. Replacing the original proposal
with its latest execution state would lose the explanation for what was approved.
Reconstructing a complete action from an old event would invent missing approval
or idempotency information.

## Decision

Keep separate resource, evidence, recommendation, action and worker-job contracts.
Resource identity remains stable under the [identity decision](adr-0001-universal-resource-identity.md).
Seal typed evidence with a content digest and embed the ordered snapshot in its
recommendation. New inputs create a new proposal; explicit supersession preserves
its predecessor. Store one action per idempotent request and keep its plan,
execution result and lifecycle separate from the proposal. Append events rather
than rewriting old events into current state.

Persist these relationships in versioned core storage. Provider extensions do not
create parallel resource or workflow tables. Apply migrations before serving work;
reject unsupported versions and broken references instead of guessing repairs.

## Consequences and limits

- Proposals remain explainable after a newer collection or restart.
- Repeated action requests can reuse an attempt without granting fresh approval.
- Embedded evidence duplicates data and favors explainability over compactness.
- A content hash detects changed content; it is not a producer signature.
- Reference-only historical recommendations remain readable but are not promoted
  to trustworthy typed evidence.
- Old action events remain history; a migration does not fabricate full actions.
- Job, action and recommendation state names must remain distinct in API/UI code.
- Tombstones and embedded evidence have no automatic deletion policy. A future
  retention policy must preserve or deliberately handle all references first.

## Alternatives rejected

A single mutable proposal/action record would mix the decision with each attempt.
A mutable “latest evidence” pointer could change the basis of an existing approval.
Treating old events as complete actions would infer facts never recorded. A separate
provider-specific workflow model would bypass common persistence and policy rules.

## Implementation and validation

See [core model contracts](../reference/core-model.md),
[store contracts](https://github.com/consize-oss/consize/blob/main/internal/store/contracts.go),
[evidence sealing](https://github.com/consize-oss/consize/blob/main/pkg/evidence/evidence.go),
[snapshot tests](https://github.com/consize-oss/consize/blob/main/internal/store/evidence_test.go), and
[restart/recovery tests](https://github.com/consize-oss/consize/blob/main/internal/safety/controller_test.go).
