# Phase 3: APT transaction preflight

## Objective and scope
Resolve logical desired-present software through Phase 2, inspect with existing APT abstractions, simulate one complete requested transaction, retain actual operations and diagnostics, classify evidence-based risks and report review requirements deterministically. No OS/package mutation, downloads, metadata refresh, apply, persistent authorization, autoremove or provider expansion.

## Constraints and decisions
- Use Go/std library and injected direct command execution. Validate operands; bound time/output; use C locale. Simulation is only a snapshot, never execution authority.
- Separate direct software, catalog dependencies and additional APT-selected packages. Report known names/architectures/versions; never invent unknown values or removal reasons.
- Review-required is a valid preview. Failed, incomplete, contradictory or ambiguous evidence is unresolved, not safely actionable. Essential/Protected metadata is evidence, and missing safety-critical evidence remains explicit.
- Only approved Foundation surfaces: README.md and docs/adr/0008-apt-transaction-preflight-and-removal-authorization.md. Preserve historic ADR0004, SAFETY, ARCHITECTURE, AGENTS and release records.
- Initial implementation prohibited commits/push; the subsequent explicit Phase 3 closure authorization below supersedes that delivery restriction without changing product scope.

## Tasks
- [x] T1 (done): Implement bounded whole-transaction APT simulation, parsing and evidence-based risks.
- [x] T2 (done): Integrate deterministic preflight CLI through existing resolution and inspection.
- [x] T3 (done): Apply the explicitly approved superseding ADR and README documentation update.
- [x] T4 (done): Complete independent validation, mutation audit, final diff inspection and native review.

## Routing
T1/T2: one delegated gentle-ai-worker at a time, test-first where meaningful. T3: isolated approved Foundation update. T4: gentle-ai-verify independent commands/diff/safety review and parent-owned native lifecycle. No parallel writers.

## Acceptance and checks
Fixtures/injected runners: installs, additional dependencies, upgrades, removals/review, combined operations, critical/protected evidence, multi-package consistency, malformed/partial output, failures/timeouts, ambiguous versions/identities, unsupported architectures, contradictions, unknown safety metadata, deterministic output, no mutating commands. Verify Phase1/Phase2 compatibility.
Fresh gofmt, go test -count=1 ./..., go vet ./..., temporary build, live read-only smokes where practical, deterministic output comparison, explicit command/mutation audit, git diff --check (including new files), final diff inspection, native review and independent verification. No live OS mutation. Git delivery is permitted only by the explicit closure authorization below.

## Progress and evidence
- Initial main HEAD 9ca3dd86fbaeb0a2ebf8fe1ee7bb858c445fbc09; clean worktree. Phase 2 baseline fcb285e29171a497d2e524c4935fe0e699b67990; intervening diff is tooling/docs only.
- User explicitly prohibits commits/push; this overrides normal ODD work-unit commits. Branch feat/apt-transaction-preflight created without source changes.
- User selected approve-foundation-update: explicitly activated /project-foundation-manager update and approved exactly ADR0008 + README. All other durable docs, historical ADR0004 and AGENTS.md remain untouched.
- Local apt-get(8) confirms --simulate makes no system changes, disables locking, supports non-root use with configuration visibility caveats, and reports Inst/Conf/Remv. Output is not a stable machine API; failures/uncertainty must preserve diagnostics.

- T1 complete: 690 production/520 test lines in APT surfaces; repeated observed RED/GREEN, fresh focused/full tests and formatting/whitespace passed. Provider.Simulate reuses Inspect, one whole sorted/deduplicated transaction, bounded execution/capture, strict records/counts/config completion/list integrity, evidence-bound Essential/Protected risks, uncertainty retained. Live simulation exit0 reports 13 installs/13 configurations; API preserves all 26 operations but assessment unresolved due omitted protection flags. --no-download removed after live incompatibility, mandatory --simulate plus APT::Get::Simulate=true retained. No refresh/download/apply or delivery. Commit: none, user-prohibited.

- T2 complete: 206-line preflight CLI helper plus injected fixture coverage; existing Run signature and Phase1/2 semantics preserved. Observed RED/GREEN twice; independent verifier fresh tests/vet/format/temp build/whitespace passed. Mint amd64 smokes: system/package bash/plan all exit0; preflight bash git is complete zero-op exit0; preflight bash git neovim exit2 retains13 installs+13configs+unresolved marker because omitted protection metadata. Repeated full stdout/stderr byte-identical. No mutation/delivery. Temporary binary /tmp/cdots-preflight-t2.ZctCDo/cdots authorized for validation. Commit none. ASSESS unassessable pending intended-untracked selection; conservative high-risk independent verification executed.

- T3 complete: explicit /project-foundation-manager update approval applied only README.md and new ADR0008. Structural readback/reference/whitespace/scoped semantic checks passed; passive docs have no meaningful RED. Historical ADR0004, SAFETY, ARCHITECTURE and managed AGENTS bytes untouched. Report-only follow-ups: AGENTS route lacks ADR0008 and ARCHITECTURE current capability intro omits preflight; separate workflows/approval needed. Future Phase4 policy not executable. Commit none.

- T4 complete: final independent fresh formatting (38 Go files), focused29 tests/84 subtests, full unit/race tests, vet, temporary build, Phase1/2 compatibility, read-only live smokes, reorder/duplicate/repeat byte-identical outputs, mutation audit, all tracked/new-file whitespace and final diff passed. Test-quality advisory fixed within two APT test files:17 integrity cases now use empty stderr and specific failure diagnostics/retained-operation assertions; no production bug or meaningful behavior RED in that strengthening. Subsequent fresh independent tests/race/vet/build/smokes passed. Tagged integration passed earlier; final repeat intentionally skipped because production/integration bytes unchanged, live CLI repeated instead.
- Native review review-93758caa1507f659:14-path Phase3 PR slice,1928 changed lines, medium tier consolidated reliability lens approved; exact acknowledgement completed authority burned. Frozen candidate6ad1034c905bbe15306df4ac2cd221b5cfd54dd3 independently matches all14 current source/docs paths. No correction required. Advisory R3-live-host-assumption at simulation_live_test.go:20-21 is informational only and deferred; no re-review warranted.
- ASSESS remains unavailable because standalone assessment requires an untracked declaration; this does not alter acknowledged native authority. Conservative unassessable/high-risk plan followed with writer self-verification plus independent verifier. No missing required functional validation. Parent spot-check preflight bash git complete zero-op/no changes and git diff --check passed.
- Final Git: feat/apt-transaction-preflight at9ca3dd86fbaeb0a2ebf8fe1ee7bb858c445fbc09,3 modified tracked files (README,cli.go,apt.go),12 untracked files (11 candidate additions+this log),empty staged diff,unchanged HEAD/index,no commits/push. Authorized temp build artifacts remain outside repo. All historical/ownership-protected docs unchanged.
- Remaining advisories: conservative unknown Essential/Protected metadata can make successful live installation previews unresolved; human-output parser deliberately narrow; duplicate configuration uncertainty can be verbose; tagged live fixture assumes host install state; Phase1 availability duplication/multiarch/partial-state advisories not broadly fixed. Live tested Mint amd64 only. AGENTS/ARCHITECTURE summary routing updates require separate authorized workflows. No implementation blockers and no asynchronous validation pending.

## Next step
Complete the user-authorized closure procedure below. Documentation-routing follow-ups remain separately owned. Do not implement Phase4 or execute any transaction.


## Phase 3 closure authorization
The user explicitly authorizes committing the reviewed Phase 3 implementation on feat/apt-transaction-preflight, normal fast-forward integration into main, normal push of main, and verification that local main, origin/main and live remote match with a clean worktree. Stop on material policy contradiction, reviewed-file mismatch or non-fast-forward divergence; never rebase, force push or rewrite history.

Approved change set: the fourteen source/documentation paths matching acknowledged candidate tree 6ad1034c905bbe15306df4ac2cd221b5cfd54dd3 plus this existing operational task record. No functionality, Phase4, advisory fixes, protected-document edits, OS/package mutations or ignored-tooling changes are authorized.

Closure revalidation passed synchronously: formatting for38 Go files, fresh full unit/race tests, vet, temporary build /tmp/cdots-phase3-close.28Lhs1/cdots, focused29 tests/84 subtests, Phase1/2 and preflight smokes, repeated/reordered/duplicate preflight stdout/stderr byte equality, mutation audit, full final diff and tracked/new-file whitespace checks. All14 reviewed blobs/modes match; all205 other tracked blobs unchanged; index was empty. Optional tagged integration was skipped for its known host-state assumption; full live CLI coverage was exercised instead. No material documentation contradiction: ADR0008 expressly supersedes only the future blanket removal policy and preserves the active Phase3 execution prohibition; historic ADR0004, SAFETY, ARCHITECTURE and AGENTS remain unchanged.

Delivery procedure: refresh/check remote history, stage exactly these fifteen paths, inspect the staged diff and verify reviewed blobs again, commit with `feat: add read-only APT transaction preflight`, switch to main and merge --ff-only, push main normally, then verify local/origin/live-remote equality, ahead/behind0/0 and clean status. The containing commit is the work-unit identity. Its final SHA and actual delivery/synchronization evidence are recorded in the separate `odd/apt-transaction-preflight/delivery` Engram topic and final report after delivery, following the Phase2 closure convention and avoiding a second bookkeeping commit.
