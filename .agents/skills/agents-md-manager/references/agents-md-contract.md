# Root AGENTS.md manager contract

This contract defines the behavior behind the three explicit modes in `SKILL.md`. It is a lifecycle and routing contract, not a documentation generator.

## Boundary and lifecycle

The only production target is `<root>/AGENTS.md`; the default root is the current working directory. An explicit root is allowed for a root operation or isolated tests. The helper rejects every symlink component in the supplied root path, resolves and uses the canonical non-symlink physical root, validates containment and the fixed basename, and accepts no arbitrary target path.

The only owned region is the half-open byte range beginning with the exact line marker `<!-- agents-md-manager:managed:start -->` and ending with the exact line marker `<!-- agents-md-manager:managed:end -->`.

| State | Evidence |
| --- | --- |
| `ABSENT` | The root file does not exist. |
| `MANAGED` | Exactly one start marker and one end marker exist as exact lines, in that order, with one managed region. |
| `UNMANAGED` | The root file exists and contains neither marker. |
| `MALFORMED` | A marker is missing, duplicated, nested, reversed, or present on a non-exact marker line. |

Unsafe target findings are internal safety results, not public lifecycle states. Reject a symlink, directory, FIFO, socket, device, or any other nonregular target. A safety classification must not follow or read the target. Reject an unsafe root directory as well. Require platform `O_NOFOLLOW` and `O_NONBLOCK` capabilities; if either is unavailable, return a structured unsafe finding before target reads or mutations rather than substituting zero flags.

Read regular files as bytes without newline or encoding normalization. Marker lines may use LF or CRLF; the marker text and line body must be exact. The replacement `payload` is the exact byte sequence inserted between the marker bytes. It includes all surrounding line-ending bytes between the start marker and end marker; the helper adds none, removes none, and normalizes none. The caller must provide a payload that leaves both markers on exact lines.

For a managed update, preserve the exact prefix through the start marker and exact suffix from the end marker onward; replace only the payload bytes. Validate that the result still contains exactly one ordered pair. A managed region is wholly owned by this skill; manual content outside it is not.

A controlled full-document repair is the only exception. It is available only for a safe `MANAGED` target, requires a full candidate with exactly one valid ordered marker pair, and requires `repairApproved: true` for that operation. The semantic layer may request it only when the user explicitly asks to repair obsolete exterior content. It must not repair `ABSENT`, `UNMANAGED`, or `MALFORMED` files, and an equivalent full candidate is a no-op. This exception replaces the complete file atomically; default managed updates still preserve exterior bytes.

`init` inspects a present file and reports its state without changing it. With `ABSENT`, it may inspect evidence and create only the root file. `update` with `ABSENT` reports that `init` is required. With `UNMANAGED`, show the proposal and ask for explicit approval for this current workflow. After approval, append one managed region at the end while leaving the existing content untouched; never silently wrap, rewrite, or reformat it. `MALFORMED` always stops without mutation and requires manual repair before another update. `audit` never writes.

## Semantic candidate and idempotence

The semantic layer owns evidence discovery and meaning. It must preserve valid manager wording and order when the relevant evidence is still current, update only guidance affected by changed evidence, and avoid stylistic whole-region rewrites. The stable conceptual order, used only for applicable nonempty sections, is:

1. `Project essentials`
2. `Commands`
3. `Context routing`
4. `Mandatory workflows`
5. `Source precedence`

This is a small ordering aid, not a rigid template. Same relevant evidence plus the same valid state produces the same candidate bytes. An equivalent payload or explicitly approved full repair candidate is a no-op: do not write, report `Mutation: none`, and report `Files changed: None`.

## Evidence router

Use this bounded navigation order to find evidence, not as a source-precedence rule:

1. Read the root README or documented index for project purpose, structure, commands, and named sources.
2. Inspect the relevant package/build manifest, task runner, scripts, CI configuration, and test configuration for exact commands and boundaries.
3. Follow only task-relevant links into architecture docs, specifications, decision records, or narrowly validated source/config files.
4. Inspect existing project instruction files and `opencode.json` only to understand already-declared routing. Preserve their ownership and configuration.

Persist a route only when its target exists, its relevance is clear, and its relationship is supported by a project index, manifest, script, task runner, CI/test config, explicit instruction, or a narrowly validated source/config claim. Use repository-relative paths. Keep each route conditional and small: `condition → path → reason`. Record commands only when their exact spelling is evidenced; never invent flags or substitute a familiar framework command.

Prefer pointers over copied prose. Do not recursively read every Markdown file. Do not enumerate skill directories, registry rows, or generic capability catalogs. A logical skill reference is permitted only when an existing project governance source makes that particular workflow mandatory; the manager never creates a catalog or invokes `project-foundation-manager`.

## Runtime context classes

Use these classes only while reasoning about evidence; never persist their labels in `AGENTS.md`, a manifest, hidden state, or a generated index:

| Class | Runtime meaning |
| --- | --- |
| `INLINE` | Small evidence needed directly for the current decision. |
| `ROUTED` | A durable pointer belongs in the managed region. |
| `SITUATIONAL` | Route only for a named task, domain, or workflow. |
| `GENERATED` | Derived output; use only when the task is about that output, never as authority by default. |
| `HISTORICAL` | Release, roadmap, or Git history; use only for history or planning questions. |
| `IRRELEVANT` | Not useful for the current task; do not load or route it. |

Treat `ROADMAP`, `CHANGELOG`, archived proposals, and historical specs as `HISTORICAL` or `SITUATIONAL` unless the project explicitly identifies a current contract. Use a docs index to select one relevant specification instead of dumping a whole tree. Treat generated registries, Graphify output, caches, and build artifacts as `GENERATED` and non-authoritative.

## Precedence and conflict handling

Look for an explicit project rule named `precedence`, `source of truth`, `authority`, or equivalent. Follow that rule as evidence and route to the file that defines it. OpenCode's instruction-loading behavior is not project source precedence: do not invent an order between README, code, specs, history, or skills.

When two existing sources make incompatible claims and no explicit project precedence resolves them, emit:

```text
CONFLICTING_CONTEXT
- Path A: <repository-relative path>; claim: <claim>
- Path B: <repository-relative path>; claim: <claim>
- Affected rule: <routing or project rule>
- Missing evidence: <precedence or verification still needed>
```

Omit the uncertain route or rule from the candidate managed region. Stable, unrelated routes may still be retained. Never choose the more recent-looking, more detailed, or more familiar source without evidence. If an explicit precedence source itself conflicts with another precedence source, report both and omit the affected guidance unless a higher project-declared rule resolves it.

For a moved pointer, search only authoritative indexes, manifests, scripts, task runners, CI, and direct references for a replacement. If evidence proves a new relative path, report the stale path and route the replacement. If no replacement is proven, report the broken source and omit the route; never guess.

## Safety and responsibility boundaries

- Manage only the root `AGENTS.md`; nested `AGENTS.md` and `CLAUDE.md` files are read-only evidence. Never create or update nested instruction files.
- Existing project-foundation-style documents, directories, and configuration are ordinary evidence when relevant. Consume them without changing them. The manager has no dependency on a foundation manager, metadata, shared state, or coupling manifest.
- Never read, print, copy, or persist values from `.env` or other secret-bearing environment files. Use safe setup documentation or non-secret configuration declarations instead.
- Do not rewrite `opencode.json`; its `instructions` entries are a separate native routing mechanism. Do not duplicate remote instructions into `AGENTS.md`.
- OpenCode discovers skills through `SKILL.md` independently from persistent project instructions. Do not reproduce the global skill catalog in the managed region.
- Use Git only for narrow, read-only evidence. Never stage, commit, push, reset, rebase, checkout, restore user work, amend, clean, or otherwise mutate Git state, including during adoption or update.
- Do not make Graphify, a daemon, watcher, hook, framework, database, OpenSpec manager, semantic project scanner, or general docs manager part of this skill.

## Mechanical helper and writes

[`../scripts/agents-md-region.mjs`](../scripts/agents-md-region.mjs) is stdlib-only and mechanical. It may classify the fixed root target, replace a managed payload, append an approved adoption, create an absent target, perform an explicitly approved managed full-document repair, reclassify fresh state at write boundaries, and verify the result. It never discovers README, docs, OpenSpec, Git, Graphify, network, skills, foundation, or semantic evidence.

At each exposed mutation boundary, the helper reclassifies from fresh bytes and target state, captures root directory `dev`/`ino`/type, and fails closed on identity changes before temporary-file creation and immediately before final rename or create. Mutation APIs do not accept caller-owned classifications, offsets, or preservation bytes. Private immutable length-plus-SHA-256 fingerprints prove the complete internally built candidate, payload, and preserved regions; public `verifyManagedRegion(root)` reports only fresh structural verification. Replacement uses a same-directory temporary file, atomic rename, and cleanup; no transaction framework or persisted state is used.

Normal callers omit the optional test-only boundary hook used by executable race regressions. Node's stdlib has no portable `openat`/dirfd-bound rename or unlink primitive, so a hostile root swap after temporary creation can strand that temp file. The helper surfaces the cleanup attempt, path, and error in structured failure details instead of claiming rollback; it uses the canonical non-symlink root, revalidates every exposed boundary, and fails closed on changes it detects. This is a proportional limitation, not an impossible-immunity claim.

Adoption requires an explicit `approved: true` input for the current operation. Approval from a generic earlier “update AGENTS” instruction is not adoption approval. The prior unmanaged bytes are the exact result prefix. The deterministic minimum separator is: empty when the original file is empty or already ends in `\n`; otherwise exactly `\n\n`. The managed block is the start marker, the caller-supplied payload, and the end marker; no prior bytes are rewritten.

Full-document repair requires the distinct explicit `repairApproved: true` input. It never infers approval from ordinary update or adoption approval. Its verification proves safe target, root identity, one ordered pair, complete candidate bytes, and exact managed payload; it deliberately makes no prefix or suffix preservation claim.

## Verification and report

Before any permitted write, retain the original root-file bytes and the proposed payload. Afterward mechanically verify a safe target, exactly one ordered marker pair with no second pair, byte-for-byte equality with the complete internally built candidate, exact payload bytes, and the required prefix/suffix or adoption-prefix preservation. An approved full-document repair instead verifies its complete candidate and payload without claiming exterior preservation. The semantic layer verifies that every persisted path is repository-relative and exists and every command has evidence. For `audit`, all stopped gates, and equivalent candidates, state `Mutation: none` and `Files changed: None`; do not report a simulated mutation.

Return a compact report with `Mode`, `State`, `Mutation`, `Routes`, `Commands`, `Preservation`, `Conflicts`, and `Next action`. Include the relevant evidence paths and distinguish `CONFLICTING_CONTEXT` from a missing or stale source.

## Activation metadata

This skill keeps `disable-model-invocation: true` and `user-invocable: true` because those are established canonical explicit-only conventions in this repository. Current OpenCode native frontmatter recognizes `name`, `description`, optional `license`, `compatibility`, `metadata`, and the V2 parser also extracts `slash`; it ignores unknown fields. Explicit-only enforcement therefore comes from this Activation Contract and the host ecosystem convention, not native OpenCode enforcement of those two keys.
