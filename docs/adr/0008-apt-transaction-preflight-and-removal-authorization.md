# ADR 0008: APT transaction preflight and removal authorization

- **Status:** Accepted (Phase 3 read-only preflight implemented; future Phase 4 authorization/apply not implemented)
- **Supersedes:** [ADR 0004](0004-no-destructive-removal-in-v0x.md) only for its blanket prohibition on future package-removal capability. ADR 0004 remains unchanged as historical evidence.

## Context

A desired-present software plan does not describe every operation APT may select for the combined request. Whole-transaction simulation can expose additional dependencies, upgrades, removals, and configurations that users need to inspect before considering execution. Hiding or automatically rejecting removals would discard relevant preview evidence; treating a successful simulation as authorization would grant authority it cannot provide.

## Decision

### Phase 3: read-only simulation and reporting

Resolve logical software IDs through Phase 2, inspect through the existing APT abstractions, and simulate one complete transaction. Distinguish directly requested software/packages, declared catalog dependencies, and additional APT-selected packages. Retain operations, known architectures/versions, diagnostics, evidence-based risks, review requirements, and explicit uncertainty; do not invent absent values, removal reasons, or negative protection flags.

Removals are valid inspectable previews requiring review, not automatically rejected solely because they are removals. Complete review-required or high-risk evidence is a valid preview, not approval to execute. Simulation success and safety assessment are separate: failed, incomplete, contradictory, or ambiguous evidence is unresolved and not safely actionable. Unknown critical, Essential, or Protected status remains explicitly unresolved rather than being treated as safe.

Use mandatory `--simulate` and `APT::Get::Simulate=true`, with bounded command time/output. Phase 3 performs no OS/package mutation, downloads, metadata refresh, installation, apply, persistent authorization, or automatic autoremove. The simulation is a non-atomic snapshot; APT's human output is not a stable machine API, the parser is narrow, and non-root configuration visibility may differ.

### Future Phase 4: operation-specific authority, not generic confirmation

The approved direction permits future removal execution only after revalidating current state and obtaining fresh, operation-specific user approval identifying the exact removals. Preview review, a prior simulation, or generic confirmation cannot supply that authority. No silent removals or automatic autoremove are permitted.

Critical, Essential, or Protected package changes require additional safeguards and cannot be authorized by generic confirmation. Unknown critical/protected status remains unresolved. This decision does not implement or specify a completed safeguard/approval mechanism, persist authorization, or guarantee execution: future apply must revalidate the transaction and obtain fresh approval rather than reuse snapshot authority.

### Supersession and current safety scope

Only ADR 0004's blanket prohibition on a future package-removal capability is superseded. Its user-file deletion and destructive-cleanup prohibitions remain intact, as do file-conflict reporting, confirmation, and backup protections.

The current Phase 3 no-removal execution invariant continues. [Safety](../SAFETY.md) remains the current mutation/file-protection contract: its execution prohibition is not waived by a removal preview or by this future direction. This ADR does not make Phase 4 executable or change the current normative execution baseline. Any future implementation must explicitly align the applicable safety contracts and safeguards before execution is available. [Architecture](../ARCHITECTURE.md) continues to describe the intended lifecycle, not an implemented apply path.

## Alternatives

Conceptual alternatives include rejecting every removal-containing simulation, displaying only directly requested packages, or treating successful simulation/generic confirmation as execution authority. These are contrasts, not claims about historical deliberation.

## Rationale

Preserving the whole transaction makes APT-selected consequences reviewable without expanding Phase 3's authority. Separating evidence, risk, and authorization avoids both suppressing useful previews and silently permitting destructive operations. Fresh, exact approval and additional safeguards bound any future removal capability.

## Consequences

`cdots preflight <software-id>...` reports complete previews, review-required/high-risk previews, or unresolved evidence while retaining useful diagnostics. Complete previews return CLI exit 0 even when review is required; unresolved evidence returns 2. Neither exit code authorizes execution. Existing system, package, and desired-present plan semantics remain unchanged; see [README](../../README.md) for usage and limitations.

Phase 4 remains an approved future direction, not implemented approval/apply behavior or a promotion of current capabilities. Simulation snapshots are not execution guarantees. ADR 0004 is retained unchanged for history; this record owns the narrow superseding decision without rewriting the current safety or architecture documents.
