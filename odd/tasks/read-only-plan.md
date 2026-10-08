# Phase 2: read-only software plan

## Objective and scope
Resolve logical software IDs, expand simple dependencies, select APT variants, reuse current-state inspection and render a deterministic read-only desired-present plan through `cdots plan <id>...`.
No installation, apply, upgrades, repository changes, Flatpak execution, profiles, TUI, dotfiles, backups or discovery. No final configuration schema or speculative ADRs.

## Baseline and decisions
- Phase 1 baseline: 59d6ad35867c246d3c9e3f76da9500855219bf50. Initial HEAD/main: b1599a8 (Engram sync); initial worktree clean.
- Implementation and final validation were left uncommitted at the user's request. The Phase 2 closure request supersedes that restriction and explicitly authorizes staging the validated Phase 2 files, one closure commit on main, and a normal push.
- Reuse apt.Provider.Inspect and existing system detection; no duplicated APT inspection.
- Tiny built-in catalog: git, bash, neovim with known APT package identifiers. Dependencies exercised by isolated fixture catalogs, not invented installation recipes.
- Preserve request/dependency declaration order with dependency-first DFS, deduplication and cycle/reference diagnostics; no unordered map iteration.
- Desired state is present only. Reliable installed version means none even without a candidate or with a newer candidate. Missing plus available candidate means install (label only); missing/unavailable means unavailable; inspection failure means error.
- Fail resolution/environment errors clearly before package inspection. No provider fallback.
- Forecast: roughly 800–1100 authored lines across implementation and tests, delivered in three coherent units plus validation. Task-size heuristic must not cause omitted tests or compressed code. No PR or commit strategy is selected because delivery is explicitly excluded.

## Tasks
- [x] T1 (done): Implement minimal software model, catalog lookup, explicit APT resolver and dependency expansion with isolated RED/GREEN tests.
- [x] T2 (done): Implement plan representation and injectable current-state comparison with isolated RED/GREEN tests.
- [x] T3 (done): Wire minimal CLI and deterministic rendering/tests; update only established durable documentation facts.
- [x] T4 (done): Independently validate full checks, read-only host/fixture smokes, determinism and mutation audit; inspect final diff and run enabled native review.

## Routing
T1–T3: delegated gentle-ai-worker; multi-file writing/preparation triggers. One writer at a time.
T4: delegated gentle-ai-verify; broad command-running verification and independent diff audit.
Parent owns task transitions, Engram mirror, native review and final report.

## Acceptance and checks
Catalog lookup; APT variant selection/missing/unsupported diagnostics; dependency expansion/dedup/order/cycles/invalid references; desired/current actions; stable plan ordering; no upgrades.
Unit tests isolated from host through injected Phase 1 boundaries. Observe RED then GREEN per behavior task.
Required: gofmt, fresh go test ./..., go vet ./..., temporary build, read-only CLI smokes including installed, available/not-installed and unknown/unavailable cases (fixtures if host catalog all installed), repeated deterministic output, git diff --check, final diff and executable mutation-surface audit.
Preserve unrelated work and Phase 1 advisories unless a concrete blocking failure proves a minimal fix necessary.
RDD switch: on (global); inspect before native START at final source candidate. Human consent and provider-owned bindings remain authoritative.

## Progress and evidence
Exploration completed via gentle-ai-explore, relevant docs and code boundaries mapped. Parent confirmed clean Git state and inspected apt.go once. CodeGraph initialized already; query returned no matching structural results.
T1 implemented internal/software/{model,catalog,resolve,software_test}.go (401 lines; 231 test lines). Observed RED: undefined API; GREEN/focused and full tests passed; gofmt and tracked diff check passed. Resolver preserves logical IDs even when package identifiers coincide. New files remain untracked; final audit must include them. Commit evidence: none, prohibited by user.

T2 implemented internal/plan/{plan,plan_test}.go (399 lines). Observed RED undefined action APIs, GREEN focused/full tests; gofmt and tracked diff check passed. Build retains ordered entries for unavailable/inspection errors and returns joined contextual error; shared package observations cached per plan. Unknown/inconsistent states cannot imply install; installed/no candidate is none. Context cancellation tested.

T3 implemented CLI plan command and fixture tests; minimal README/ARCHITECTURE/CONFIGURATION/AGENTS status updates (+253/-23 lines). RED old usage error; GREEN CLI tests and fresh full suite passed. gofmt, vet, temporary build, existing command smokes and diff check passed. Host Mint amd64: bash 5.2.21-2ubuntu4 and git 1:2.43.0-1ubuntu7.3 => none; neovim missing/candidate 0.9.5-6ubuntu2 => install label. Unknown/missing plan args => exit 2. Unavailable/error/missing APT/unsupported OS covered with injected fixtures. No system changes.

Native review selected 12 source/documentation files, excluding this task log. Candidate tree 6179cb8ccd03361681eb39273062d9d2ce0dca3b; lineage review-a3c42ae89c124231. Native medium tier (1076 authored diff lines), one reliability review approved with no correction; exact acknowledgement completed and authority burned. No Git stage/commit/push. Initial ASSESS unavailable until intended-untracked scope selected; conservative independent verification already delegated. Parent spot-check repeated plan bash git neovim and diff check successfully. Engram full mirror readback matches after trailing-newline normalization.

T4 independent verification passed formatting, fresh full tests, vet, temporary build, all asserted smokes, repeated stdout/stderr byte comparison, tracked/untracked whitespace checks and full mutation/diff audit. No implementation blocker found. Coverage gap: no single fixture combines installed + missing/available + unavailable/error entries; existing fixtures split the mixtures. T4 remains open pending this test and revalidation. No executable mutation APIs found; only Phase1 uname, dpkg-query, apt-cache policy and dpkg compare-versions. No unrelated file/index/HEAD changes observed during verification.

Coverage gap closed with TestPlanRetainsMixedInstalledAvailableAndUnavailableEntries (+27 test lines), exact retained bash none/git unavailable/neovim install and all inspector calls. Coverage-only change has no meaningful RED; fresh focused/full tests, vet, gofmt and whitespace checks passed. Production code unchanged.
Updated candidate tree 435bf69db0ac6d24976eb8c5a4b524c33785df4a; lineage review-66534f3ef612a4f2, medium tier (1103 diff lines). Initial fresh START hit expired consent and explicitly created no lineage; tool-directed fresh inspect/START succeeded. Reliability review approved; exact acknowledgement completed and authority burned. No Git commits/staging/push.

## Final synchronous validation
Status: READY_TO_CLOSE.
At the user's final-validation request, completed a new foreground gentle-ai-verify run; no asynchronous work remains. The retained earlier mixed-state verification had already passed; its scope concern was this parent's intentionally excluded task-log update, not a production defect.
- gofmt output empty; fresh full go test -count=1 ./..., focused mixed-state test and verbose software/plan/CLI fixtures all passed.
- go vet ./... and temporary build /tmp/cdots-phase2-verify.puQ9C9/cdots passed.
- Asserted no-args/system/package bash/package unavailable/plan bash git neovim/duplicate selection/unknown ID/missing args smokes passed. Host bash/git installed => none; neovim missing with 0.9.5-6ubuntu2 candidate => install label.
- Repeated plan stdout and stderr byte-identical. Dependency expansion/dedup/order/cycles/references/provider/inspection-error fixtures passed, including the mixed installed/available/unavailable plan.
- Tracked git diff --check and all eight untracked-file whitespace checks passed; full diff and new files inspected.
- Full executable-path audit confirmed only read-only Phase1 commands; no install/upgrade/remove/autoremove/repository/Flatpak/dotfile/filesystem-apply path.
- All 12 reviewed source/docs files match acknowledged native tree 435bf69db0ac6d24976eb8c5a4b524c33785df4a. The separate task log does not invalidate that identity.
- All 23 unchanged tracked files match HEAD. Verifier preserved 36 file hashes plus status/index/HEAD; no repository binary.
Native review: review-66534f3ef612a4f2 approved, exact acknowledgement completed and authority burned. No new native review was needed: no reviewed bytes changed during this validation request.
Blocking defects: none. Failed/skipped/pending validation checks: none. Prior Phase1 availability-duplication/multiarch/partial-state advisories remain nonblocking and unchanged. Live smoke coverage is Mint amd64; other supported environments/errors use isolated fixtures.
Git: main at b1599a8160494d73ae5c06c9538a403298bb8920; five modified tracked files and eight untracked files; no staged changes or new commits/push. This final request changes only this parent-owned progress record and its Engram mirror.

## Phase 2 closure authorization
The user authorized one closure commit on main with message `feat: add read-only software resolution and planning`, followed by a normal push and local/origin/live-remote equality, ahead/behind 0/0 and clean-worktree verification.
No new functionality, Phase 3, advisory fixes, history rewrites or system mutations are authorized. Existing source/docs must still match the validated candidate; only this task log receives the superseded delivery instruction and closure bookkeeping.
Closure verification passed: fresh formatting, tests, vet, temporary build /tmp/cdots-closure-verify.uselz2gn/cdots, asserted read-only CLI smokes, repeated-plan stdout/stderr byte comparison, mutation and documentation/router integrity audits, full tracked/new-file diff inspection and whitespace checks. All 12 source/docs blobs still match acknowledged tree 435bf69db0ac6d24976eb8c5a4b524c33785df4a. No unrelated state changed; ignored regular tooling files and tracked Engram config preserved. Exact staged-candidate inspection and delivery follow under the authorization below. Existing ignored .atl/.codegraph state and tracked Engram config remain excluded.
Delivery procedure: include only the inspected 13 Phase 2 files, validate the exact staged candidate, create the authorized commit, push main normally, then verify synchronization and cleanliness. The containing Git commit is the work-unit identity; its final SHA, push and synchronization evidence are recorded in the separate `odd/read-only-plan/delivery` memory and final report after delivery, avoiding a second bookkeeping commit.

## Next step
Complete closure validation and inspect the exact staged candidate before the authorized commit/push. Do not start Phase 3.
