---
name: agents-md-manager
description: "Manage root AGENTS.md through explicit init, update, and audit workflows with byte-safe preservation and user-approved full-document repair."
disable-model-invocation: true
user-invocable: true
license: MIT
metadata:
  author: "Codenburg"
  version: "1.1"
---

## Activation Contract

Use only when the user explicitly invokes `/agents-md-manager init`, `/agents-md-manager update`, or `/agents-md-manager audit`. Manage the root `AGENTS.md` lifecycle as an evidence-based context router; nested instruction files are read-only evidence.

## Hard Rules

- Expose exactly `init`, `update`, and `audit`; manage only the root `<root>/AGENTS.md` and exactly one ordered marker pair.
- Use Git only as narrow read-only evidence. Never stage, commit, push, reset, rebase, checkout, restore user work, amend, clean, or otherwise mutate Git state; this applies to every mode, including adoption and update.
- Preserve the prefix through the start marker and suffix from the end marker onward byte-for-byte by default. A full-document repair is allowed only for a `MANAGED` target when the user explicitly requests it and the helper receives `repairApproved: true`; otherwise never rewrite exterior bytes.
- Treat `ABSENT`, `MANAGED`, `UNMANAGED`, and `MALFORMED` as the only public lifecycle states. Report unsafe targets as a separate internal safety finding, not as a fifth lifecycle state.
- Reject any symlink component in the supplied root path, use the canonical physical root, capture its directory identity, and fail closed on detected root or target changes. Require safe-open capabilities; never read or mutate with missing no-follow/nonblocking primitives.
- `audit` never writes. `init` writes only `ABSENT`; `update` writes only `MANAGED`, or appends to `UNMANAGED` after explicit approval for this current workflow. `MALFORMED` always stops without mutation.
- Route only existing, evidence-backed, repository-relative sources. Read selectively; never inspect secrets, recursively dump Markdown, scan projects semantically, enumerate skills, or create foundation/Graphify/catalog integrations.
- Use project-declared precedence when present. Otherwise emit `CONFLICTING_CONTEXT` with both paths, claims, affected rule, and missing evidence, then omit uncertain guidance.
- Use the stdlib-only mechanical helper at [`scripts/agents-md-region.mjs`](scripts/agents-md-region.mjs); it classifies, preserves, writes, and verifies bytes but never discovers semantic evidence.

## Decision Gates

| Mode/state | Action |
| --- | --- |
| `init` + `ABSENT` | Inspect bounded evidence and create the root managed region only. |
| `init` + any present state | Report the state; change nothing. |
| `update` + `MANAGED` | Replace only the exact interior; an equivalent candidate is a no-op. With explicit user-requested exterior repair and `repairApproved: true`, replace one valid full-document candidate. |
| `update` + `UNMANAGED` | Show the semantic proposal, then require explicit current-workflow adoption approval. |
| `update` + `MALFORMED` or `ABSENT` | Stop and report; change nothing. |
| `audit` + any state | Report evidence and preservation checks; change nothing. |

## Execution Steps

1. Load [`references/agents-md-contract.md`](references/agents-md-contract.md) and mechanically classify the fixed root target before reading project content. If it is unsafe, do not follow or read it; report the safety finding separately from lifecycle state.
2. Discover semantic evidence selectively: high-signal indexes, manifests, task runners, CI/test configuration, and task-relevant docs or source. Record routes, exact evidenced commands, precedence, stale pointers, and conflicts.
3. Build a candidate payload in the applicable small conceptual order: `Project essentials`, `Commands`, `Context routing`, `Mandatory workflows`, `Source precedence`. Omit empty sections; this is an ordering aid, not a rigid template. Keep runtime context classes ephemeral.
4. Apply the mode/state/adoption gate, then use the helper for a safe write or a no-op. Use full-document repair only for the explicit user-requested exception; revalidate at each mutation boundary, preserve regular-file mode, and use same-directory atomic replacement.
5. Use the helper to verify target safety, one exact ordered marker pair, complete candidate bytes, and exact payload. Verify default prefix/suffix or adoption-prefix preservation; report full-document repair as the approved exception.

## Output Contract

Return `Mode`, public `State`, `Mutation`, `Routes`, `Commands`, `Preservation`, `Conflicts`, and `Next action`. For read-only, stopped, or semantically idempotent results, state exactly `Mutation: none` and `Files changed: None`. Name `CONFLICTING_CONTEXT` findings exactly.

## References

- [`references/agents-md-contract.md`](references/agents-md-contract.md) — marker, routing, evidence, safety, and verification contract.
