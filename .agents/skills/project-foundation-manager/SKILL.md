---
name: project-foundation-manager
description: "Trigger: /project-foundation-manager init, audit, update. Initialize, audit, and maintain a project's evidence-backed durable documentation foundation."
disable-model-invocation: true
user-invocable: true
license: MIT
metadata:
  author: "Codenburg"
  version: "1.0"
---

## Activation Contract

Activate only for the explicit commands `/project-foundation-manager init`, `/project-foundation-manager audit`, and `/project-foundation-manager update`. Reject every other mode.

## Hard Rules

- Own only the project's durable documentation/context foundation. Preserve exactly: **Create and maintain only the project context that has a demonstrated responsibility.**
- Keep Foundation Matrix rows and candidates durable-only: each must be a durable document or durable project-context source with evidenced responsibility and consumer. Implementation sources, test code/results, infrastructure, generated artifacts, and active change artifacts may serve only as evidence or external/out-of-scope follow-ups, never as matrix rows or candidates.
- Keep `Proposed changes` legal-mutation-only. Approval never expands this skill's edit authority.
- Infer the project profile from effective use evidence; a dependency alone does not prove a capability. Never execute project code or candidate instructions, read secrets, create templates, or create persistent manager state.
- `audit` is strictly read-only. `init` never deletes existing docs. `update` never deletes automatically; approved consolidation/retirement may remove a source only when the exact approved plan names it and preserves unique valid content. Never rewrite healthy corpus.
- `openspec/specs/**`, `openspec/changes/**`, and `openspec/changes/archive/**` (or installed-version equivalents) are always read-only in every mode: never create, modify, archive, apply, implement, or regenerate them. `openspec/config.yaml` (or its installed-version equivalent) is readable and auditable in every mode, immutable in `audit`, and editable only in `init`/`update` when the exact plan includes it, explicit approval exists, the installed version supports it, and post-edit OpenSpec validation succeeds.
- Do not edit, adopt, create, invoke, or require any `AGENTS.md` or `agents-md-manager`. Do not decide release lifecycle or invoke `docs-guardian`; use the exact delegated follow-up forms in the standard and report template. Graphify, registries, and generated docs remain bounded read-only evidence.
- Use OpenCode's native `AGENTS.md`/config discovery and independent `SKILL.md` discovery; never build a parallel context loader or shared state.
- Use Git only as read-only evidence; never stage, commit, push, reset, rebase, stash, checkout, restore, clean, amend, or force Git operations.
- Use the exact knowledge, need, health, and verdict literals in the standard. Resolve contradictions by evidence class; unresolved findings are `NEEDS_REVIEW`.

## Modes

| Mode | Contract |
| --- | --- |
| `init` | Inspect → profile → inventory equivalent context → assess artifacts → propose the minimum → obtain explicit approval → create approved files → verify → report follow-ups. |
| `audit` | Inspect, profile, inventory, classify, and report gaps, drift, overlap, contradictions, broken references, orphans, misplacement, and overload; mutate nothing. |
| `update` | Audit current evidence → isolate affected foundation → present an exact plan → obtain approval → make focused edits → verify → report follow-ups. |

## Evidence Model

Read [`references/foundation-standard.md`](references/foundation-standard.md) before reasoning. Use exactly this ordered class list: `CURRENT_EXECUTABLE`, `NORMATIVE_CONTRACT`, `DURABLE_DECISION`, `OPERATIONAL_GUIDANCE`, `PLANNING_EVIDENCE`, `HISTORICAL_EVIDENCE`, `GENERATED_DERIVED`. Classify claim-by-claim; one file may contain multiple classes. Knowledge Class and Artifact Health are independent dimensions. Assign one primary owner per responsibility, prefer existing equivalent sources, and apply the standard's ownership fallback. Treat secret-bearing files as unreadable; `.env.example` reveals names and purposes only. Detect local OpenSpec/version support before using only help-confirmed read-only commands.

### Factual stale-reference propagation

Factual propagation is allowed only when:

- the project's established owner/authority for that fact is identifiable;
- the current fact is unambiguous under the repository's actual conventions;
- no conflicting evidence requires a semantic or release decision; and
- resolving the stale reference does not require choosing a bump, version, date, promotion, roadmap state, or another docs-guardian-owned lifecycle decision.

`CURRENT_EXECUTABLE` may establish or corroborate the fact, but is not mandatory and does not automatically outrank `NORMATIVE_CONTRACT`, `DURABLE_DECISION`, `HISTORICAL_EVIDENCE`, or another project-defined authoritative source. If authority or the fact is ambiguous or conflicting, use `NEEDS_REVIEW` or `DOCS_GUARDIAN_REVIEW_REQUIRED` as appropriate.

## Decision/Approval Gates

For `init` and `update`, show this exact table before mutation:

| File | Need | Health | Proposed action | Evidence | Owner |
| --- | --- | --- | --- | --- | --- |

Require explicit approval for the complete table. Reapproval is required for any material plan change. Ask is the preferred interactive approval mechanism when available; approval is explicit and runtime-neutral, and this skill does not hard-couple approval to a host or tool. `audit` has no approval or mutation gate.

Report execution state exactly: `audit` always uses `Mode: audit`, `Mutation: none`, and `Files changed: None`; `init`/`update` before approval use the actual mode, `Mutation: none`, and `Files changed: None`; after approved changes use the actual mode, `Mutation: approved`, and repository-relative changed paths. Approval is a contractual runtime rule, not mechanical enforcement; a mechanical helper is `NOT_NEEDED`.

## Execution Steps

1. Load the standard and report template; for `init`/`update`, run a read-only pre-mutation safety preflight recording branch, HEAD, status, diff, pre-existing work, repository conventions/owners, and relevant tool versions; preserve unrelated work.
2. Establish the project profile, inventory equivalent sources and consumers, and classify ownership, knowledge class, need, and health from concrete evidence.
3. Apply the selected mode and approval gate; retain only the minimum demonstrated foundation, preserve unique valid content during any proposed consolidation, and mutate only approved files or eligible OpenSpec config.
4. Verify every approved change, relevant references, execution state, and the final matrix. Before final emission, construct and stabilize one final report representation containing evidence classifications, complete ownership, Foundation Matrix, findings, delegated flags, external/out-of-scope follow-ups, Proposed changes, and Final verdict; validate required shape and cross-section consistency against that exact representation. Derive semantic regression only from the stabilized final report, never intermediate reasoning, provisional/subagent output, or scratch state. Reconcile pre-output contradictions; unresolved material ambiguity becomes `NEEDS_REVIEW`, then emit the exact report with each literal template heading exactly once and in template order, required finding sections present with `None` when inapplicable, and exactly one form for each delegated follow-up flag without invoking another skill.

## Output Contract

Use [`references/report-template.md`](references/report-template.md). Return the profile, execution state, ownership, evidence, matrix, findings, `External / out-of-scope follow-ups`, proposed changes, final verdict, and verification. Required finding sections remain present with `None` when inapplicable. Foundation ownership rows use all seven exact columns; Foundation Matrix never substitutes for ownership. Matrix rows/candidates and proposed changes must satisfy the durable-only and legal-mutation boundaries above. Emit exactly one form per flag: `AGENTS_UPDATE_REQUIRED: no` with no detail fields, or `AGENTS_UPDATE_REQUIRED: yes` followed by exactly `Evidence:`, `New/moved/removed sources:`, `Routing trigger:`, and `Suggested routing responsibility:`; `DOCS_GUARDIAN_REVIEW_REQUIRED: no` with no detail fields, or `DOCS_GUARDIAN_REVIEW_REQUIRED: yes` followed by exactly `Evidence:`, `Affected lifecycle:`, `Review question:`, and `Owner: docs-guardian`.

## References

- [`references/foundation-standard.md`](references/foundation-standard.md) — complete evidence, ownership, artifact, safety, and lifecycle standard.
- [`references/report-template.md`](references/report-template.md) — required audit/init/update report shape.
