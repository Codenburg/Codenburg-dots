# Read-only CLI implementation

## Objective
Implement the first tested, standard-library-only Go cdots CLI: system detection and APT package inspection, without operating-system mutations.

## Scope and decisions
- Module: github.com/Codenburg/Codenburg-dots, derived from origin; Go 1.22 compatibility.
- Explicit supported IDs: debian, ubuntu, linuxmint; ID_LIKE alone does not authorize unsupported derivatives.
- CLI: cdots (system summary), cdots system, cdots package <name>.
- Separate system detection, small package domain, and APT adapter; no future planner/apply/provider framework.
- Only uname -m, dpkg-query, apt-cache and dpkg version comparison commands; never sudo or package mutations.
- Implementation was initially left uncommitted at the user's request. The Phase 1 closure request supersedes that restriction and authorizes one Phase 1 commit followed by a normal push of main.

## Tasks
- [x] T1 (done): Implement minimal CLI, isolated system/APT logic and deterministic tests using observed RED/GREEN; update README for established behavior only.
- [x] T2 (done): Independently validate formatting, tests, vet, build, host read-only smoke checks, installed/missing package inspection and mutation-surface audit.
- [x] T3 (done): Inspect final diff, run enabled native review, reconcile documentation and report Git status and limitations.

## Acceptance and checks
Support Debian/Ubuntu/Linux Mint; normalized amd64/arm64 with raw diagnostic architecture; detect APT tooling; installed and candidate versions when reliable; clear environmental errors, no panics. Unit tests must not rely on host packages. Required checks: gofmt, go test ./..., go vet ./..., build cdots into temporary directory, CLI smoke checks, git diff --check, final diff audit. No dependency frameworks or speculative ADRs.

## Evidence and progress
Initial main worktree clean; Go 1.22.2 available; RDD enabled globally. Explorer read authoritative foundation; CodeGraph query returned no Go packages.

## Verification outcome and next step
T1 and correction worker reported observed RED/GREEN. Independent final T2 revalidation passed: gofmt clean, fresh full tests, vet, build, no-args/system and installed/missing smoke, diff check and full mutation audit. Host Mint amd64/raw x86_64, bash installed=candidate 5.2.21-2ubuntu4, missing unavailable. Parent repeated final system smoke and diff check successfully. Native four-lens review approved review-595aa2bb055b727d; exact acknowledgement completed and authority burned. Task log excluded from candidate. Non-blocking advisories R2-001 availability duplication, R3-multiarch and R4-partial-package-state retained as later work; no correction offered. Separate ASSESS unavailable due untracked declaration; fallback high-risk independent verification already satisfied. No remaining concrete blockers. Go1.22.2 host validation; Debian/Ubuntu and arm64 classifications tested with deterministic fixtures, not separate live hosts. No installation, mutation or metadata refresh performed. Implementation handoff status (historical): modified AGENTS.md, README.md, docs/ARCHITECTURE.md; untracked cmd/, go.mod, internal/, odd/. No stage, commit or push was performed during implementation.

## Phase 1 closure
Final closure validation passed again: gofmt check, fresh unit tests, go vet, temporary build, no-args/system and installed/missing package smoke checks, full scope/mutation audit, and git diff --check. Existing native review and healthy foundation/router audits remain unchanged; only this task record needed the superseded delivery instruction corrected. The Phase 1 commit includes code, tests, narrowly scoped documentation updates, and this task record. Unchanged tracked .engram/config.json and ignored .codegraph/ and .atl runtime artifacts are excluded. Known non-blocking advisories remain follow-ups, not closure work. No Phase 2 work is authorized or included.

Delivery procedure: inspect the staged Phase 1 diff, create one commit with message `feat: add read-only Debian and APT inspection`, push main normally, then verify HEAD, origin/main and remote main agree and the worktree is clean. The containing Git commit is the work-unit identity; final SHA, push outcome and synchronization evidence are recorded in the closure session memory/report after delivery so no follow-up bookkeeping commit is needed.
