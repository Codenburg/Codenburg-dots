# ADR 0003: Declarative and idempotent desired state

- **Status:** Accepted (decision not implemented)

## Context

A user may request software and dotfiles that are already present or configured. Reapplying a request should not cause needless churn or obscure the difference between desired and observed state.

## Decision

Model selected configuration declaratively as desired state and make application idempotent: when the requested state is already satisfied, avoid unnecessary work.

## Alternatives

Conceptual contrasts include imperative repeated installation steps or applying every requested action regardless of current state. These are not asserted to have been historically evaluated.

## Rationale

Comparing desired and observed state supports reviewable plans and avoids needless operations. Detection and planning remain read-only; application follows explicit confirmation.

## Consequences

A future engine needs a way to inspect current state, resolve desired state, and report planned changes. The schema and implementation do not yet exist. Declarative reconciliation does not permit destructive removal or override the protections in [ADR 0004](0004-no-destructive-removal-in-v0x.md).
