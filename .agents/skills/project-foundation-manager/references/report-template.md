Emission rule: emit every literal heading exactly once and in this template order; never substitute a heading. Keep every required finding section present with explicit `None` when no finding applies, without inventing content.

# Project profile

- **Purpose and role:**
- **Project shape and lifecycle:**
- **Demonstrated capabilities and consumers:**
- **Bounded evidence inspected:**
- **Safety exclusions:**
- **OpenSpec boundary:** `openspec/specs/**`, `openspec/changes/**`, and `openspec/changes/archive/**` (or installed-version equivalents) are always read-only in every mode: never create, modify, archive, apply, implement, or regenerate them. `openspec/config.yaml` (or its installed-version equivalent) is readable and auditable in every mode, immutable in `audit`, and editable only in `init`/`update` when the exact plan includes it, explicit approval exists, the installed version supports it, and post-edit OpenSpec validation succeeds.

# Execution state

- `Mode`: `audit` | `init` | `update`
- `Mutation`: `none` | `approved`
- `Files changed`: `None` | repository-relative paths
- **Rules:** `audit` always reports `Mode: audit`, `Mutation: none`, `Files changed: None`; `init`/`update` before approval report the actual mode, `Mutation: none`, `Files changed: None`; after approved changes report the actual mode, `Mutation: approved`, and repository-relative changed paths.

# Foundation ownership

| Responsibility | Primary source | Owner | Consumers | Creation condition | Update triggers | Retirement condition |
| --- | --- | --- | --- | --- | --- | --- |

Ownership rule: every row uses exactly the seven columns `Responsibility`, `Primary source`, `Owner`, `Consumers`, `Creation condition`, `Update triggers`, and `Retirement condition`. Every column must have a value; unknown evidence uses explicit `NEEDS_REVIEW` or a brief uncertainty description. Do not abbreviate the table or use the Foundation Matrix as a substitute. If no ownership row is evidenced, retain this heading and write `None`.

# Evidence model

- **Current executable evidence:**
- **Normative contracts:**
- **Durable decisions:**
- **Operational guidance:**
- **Planning evidence:**
- **Historical evidence:**
- **Generated/derived evidence:**
- **Claim classification:** classify claim-by-claim; one file may contain multiple Knowledge Classes. Knowledge Class and Artifact Health are independent dimensions.
- **Precedence or unresolved evidence conflicts:**

# Foundation matrix

Foundation Matrix rows and candidates are durable-only: each must be a durable document or durable project-context source with evidenced responsibility and consumer. Durable documents such as `tests/README.md` qualify when they own a demonstrated testing responsibility; test code/results, implementation sources, infrastructure, generated artifacts, and active change artifacts are evidence or external/out-of-scope follow-ups only, never rows or candidates. An external follow-up ACTION is never a matrix candidate or a Proposed change; its underlying durable external-owner SOURCE may be assessed independently when it satisfies durable membership, without transferring ownership or mutation authority. Evaluate `docs/security.md` and root `SECURITY.md` as independent contracts: internal security architecture/controls versus public vulnerability-reporting policy.

| Artifact | Need | Health | Evidence | Owner | Action |
| --- | --- | --- | --- | --- | --- |

# Missing foundation

- **Artifact:**
- **Need:**
- **Evidence:**
- **Consumer and owner:**
- **Minimum action:**

# Stale documentation

- **Artifact:**
- **Current evidence:**
- **Stale claim:**
- **Health action:**

# Overlap

- **Responsibilities involved:**
- **Sources:**
- **Primary-home recommendation:**
- **Unique valid content to preserve:**
- **Approval required before consolidation/deletion:** yes

# Contradictions

- **Finding:**
- **Evidence class A and claim:**
- **Evidence class B and claim:**
- **Affected responsibility:**
- **Resolution or missing evidence:**

# Broken references

- **Source:**
- **Broken target:**
- **Evidence of intended replacement, if any:**
- **Action:**

# Misplaced / overloaded context

- **Source:**
- **Responsibility that belongs elsewhere:**
- **Distinct responsibilities combined:**
- **Proposed primary homes:**

# Delegated follow-ups

Emit exactly one form for each flag. The variants below are alternatives; include only one form per flag in a report.

AGENTS no form:

```text
AGENTS_UPDATE_REQUIRED: no
```

AGENTS yes form:

```text
AGENTS_UPDATE_REQUIRED: yes
Evidence:
New/moved/removed sources:
Routing trigger:
Suggested routing responsibility:
```

DOCS guardian no form:

```text
DOCS_GUARDIAN_REVIEW_REQUIRED: no
```

DOCS guardian yes form:

```text
DOCS_GUARDIAN_REVIEW_REQUIRED: yes
Evidence:
Affected lifecycle:
Review question:
Owner: docs-guardian
```

# External / out-of-scope follow-ups

Record work outside this skill's legal mutation scope here as an external follow-up ACTION, not in the Foundation Matrix or Proposed changes. Use one compact entry per follow-up:

- **Recommendation:**
- **Evidence:**
- **External owner:**
- **Boundary reason:**

Each ACTION entry in this section is never a Foundation Matrix candidate or Proposed change. Assess its underlying durable external-owner SOURCE independently under the Foundation Matrix rule above; that assessment does not transfer ownership or mutation authority.

# Proposed changes

List only legal mutations within this skill's authority. Approval never broadens that authority. Ask is the preferred interactive approval mechanism when available; approval remains explicit and runtime-neutral and is not hard-coupled to a host or tool.

| File | Need | Health | Proposed action | Evidence | Owner |
| --- | --- | --- | --- | --- | --- |

- **Approval:** `PENDING` / `APPROVED` / `NOT_REQUIRED` / `REAPPROVAL_REQUIRED`
- **Files approved for mutation:**
- **Files intentionally unchanged:**
- **Relevant diff after mutation:**
- **Verification:**
- **OpenSpec configuration guard:** list an OpenSpec config edit only when the exact approved plan includes it, the installed version supports it, and post-edit validation succeeds; specs/changes/archive remain read-only.

# Final verdict

`HEALTHY` / `IMPROVEMENTS_RECOMMENDED` / `ACTION_REQUIRED` / `NEEDS_REVIEW`

Final semantic/regression assessment derives from the stabilized final emitted report and cannot contradict its ownership, Matrix, findings, follow-ups, flags, `Proposed changes`, or verdict. Reconcile any contradiction before emission or mark the material point `NEEDS_REVIEW`.

- **Reason:**
- **Unresolved decisions:**

List under `Unresolved decisions` only unresolved matters that materially block foundation-health assessment, a legal mutation plan, or a necessary external-owner decision for a material finding. Unrelated engineering debt merely discovered as evidence is excluded.
