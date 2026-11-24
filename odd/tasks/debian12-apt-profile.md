# Debian 12 APT profile compatibility
Work only in the isolated checkout; keep transferred evidence read-only. No delivery or live APT.

## Specs
- S1: “Verify the file hashes, inspect the full profile and integrate sanitized regression fixtures without altering the original evidence.”
- S2: “Preserve all previously agreed security invariants and decisions. Keep tests offline, avoid VM/APT transactions, network access, commits, pushes and merges.”
- S3: “Report confirmed findings, corrections, validation results and any remaining blockers before the next non-installing VM retest.”

- S4: “Use them as read-only, offline, source-backed references.”
- S5: “Resolve the remaining source-backed questions autonomously, and report any genuine security or scope blockers.”

- S6: “Focus on the remaining blockers: deterministic `apt-config dump` format, legitimate Debian executable-path defaults and trust boundaries, effective `apt-get -c` configuration and Binary scoping, configuration drift, and the exact apt-listchanges protocol/FD contract, including potential mail and seen-state side effects.”
- S7: “I explicitly authorize **one scoped `gentle-ai-verify` worker** for independent, read-only verification of the relevant security changes (Risk: item 2).”
- S8: “If the assessment cannot isolate the intended scope, report the limitation instead of expanding it.”

## Tasks
- T1 [done] — S1: Verify archive, manifest and full profile; parent, no commit.
- T2 [done] — S1–S2: Pin sanitized fixtures; admit only the exact inert Debian `01autoremove` contents through the directive check; parent, no commit.
- T3 [done] — S2–S3: Offline Go checks passed; remaining proof requirements recorded; parent, no commit.
- T4 [blocked] — S2–S3: Independent verification and native profile proof are still missing; no VM retest recommended.
- T5 [done] — S1–S5: Confirm local source checksums; harden and generalize comment handling and recognize only the observed literal regex values; run offline regressions, no commit.

- T6 [done] — S6: Pin dump framing, prove retained hook's effective protocol and preserve downstream fail-closed behavior; parent, no commit.
- T7 [blocked] — S7–S8: One authorized verifier completed static review; independent runtime checks did not run without a pinned candidate.
- T8 [done] — S2, S6: Address both static findings and verify offline; no second worker authorized.

## Log
### L1 — Original request
The real Debian 12 VM evidence has been transferred successfully to the Linux Mint host.

Evidence directory:
`/home/ace/Proyectos/cdots-evidence/vm/ev`

Original archive:
`/home/ace/Proyectos/cdots-evidence/ev.tgz`

Archive SHA256:
`3eb8aa7ff9db5ba897de5848ec0e444af1e279b50df37547580d609a23638aa5`

Contents include native APT configuration files, the complete `apt-config dump`, `apt-listchanges` configuration, version information and `SHA256SUMS`.

Continue in `/home/ace/Proyectos/cdots-apply-absence-fix` only. Keep the original main untouched.

Use the real evidence as read-only input for the agreed Debian 12 compatibility and execution-security work. Verify the file hashes, inspect the full profile and integrate sanitized regression fixtures without altering the original evidence.

Preserve all previously agreed security invariants and decisions. Keep tests offline, avoid VM/APT transactions, network access, commits, pushes and merges.

Report confirmed findings, corrections, validation results and any remaining blockers before the next non-installing VM retest.

### L2 — Evidence boundary
Archive digest and all eight manifest checks match; all nine regular archived files match the extracted bytes. Archive and host extraction report different file UIDs/GIDs; transferred bytes do not attest the VM's live execution-path trust. The archive contains config and versions, not the exact package sources or an effective apt-get protocol trace.

### L3 — Focused regression and remaining execution blockers
The pre-fix run printed the expected `01autoremove` rejection before the foreground wrapper timed out. The post-fix focused run exited 0: all five native fragments passed the directive check, different name/changed hash comment/added directive controls rejected, and the complete dump still failed closed at the encoding check. This exception changes neither the trusted-path predicate nor downstream hook/transaction checks.

The snapshot shows literal backslashes in `Dir::Ignore-Files-Silently`, Debian `Dir::Bin` defaults including list-shaped solver/planner paths, and the two ordered native hooks. `apt-listchanges` uses protocol 2 and InfoFD 20; debconf follows it. Its captured configuration selects a pager, news, local root email, and a seen-state database. Do not assume the hook is harmless or infer what ran from its shell wrapper exit status. The current guard retains protocol 3 and InfoFD 0; no native execution ordering or transport was changed.

Before recommending a non-installing VM retest, resolve the safe handling of dump encoding and binary defaults, prove the effective apt-get profile and disposition of apt-listchanges against exact offline sources or a separately reviewed non-installing probe, and obtain the enriched trust diagnostic if the path rejection persists. Do not use `cdots apply` as this probe.

Read-only native assessment classified the edit as risk item 2 and could not assess the candidate because untracked scope was undeclared. No native review or independent worker was run under the offline/prior no-new-worker restrictions; no receipt or independent approval is claimed.

### L4 — Final offline checks
Post-fix focused suite and full `go test ./...` returned 0. `go vet ./...`, formatting, and `git diff --check` passed. Both prior absence-fix files retain their recorded SHA256 values. The archive and all eight manifest entries still pass revalidation. No VM, native APT transaction, network, commit, push, merge, or original-main change. Offline self-verification is complete; Debian compatibility and security acceptance are not.

### L5 — Continuation evidence and native review scope
No matching APT source was found in the authorized checkout and transferred evidence tree. The historical T3b notes record tagged-source ordering (`APT_CONFIG`, fragments, main; then Binary/command scope before sequential CLI `-c`/`-o`) and guard requirements, but do not document the tokenizer or dump serializer precisely enough to justify accepting general hash comments or backslashes. The transferred fixture records seven literal `\.` values in `Dir::Ignore-Files-Silently`, distinct from native hook and path settings. The absolute solver/planner list entries and compressor executable defaults cannot be treated as generic trusted redirects. The hook list/options in the snapshot are not a trace of effective `apt-get` behavior. No production rule or fixture was changed in this continuation.

Target-scoped review INSPECT returned `intended_untracked_selection_required` before START. The digest covers eligible path names, and selected file contents are hashed only at freeze. Explicit selection or exclusion addresses untracked scope only; the tracked workspace projection also contains `skills-lock.json` and the previous `apply_validation.go` change. No scope selection, START, staging, cleanup, or unrelated edit was performed. T4 was blocked on the exact source bundle location and independent verification authority at this point.

### L6 — Source bundle supplied
The exact Debian source archives are now available locally at `/home/ace/Proyectos/cdots-evidence/debian-sources/`:

- APT 2.6.1
- apt-listchanges 3.24
- debconf 1.5.82

Use them as read-only, offline, source-backed references. Continue the existing Debian 12 compatibility investigation in `/home/ace/Proyectos/cdots-apply-absence-fix`, preserving all agreed security invariants.

Keep original main, VM, original evidence, and unrelated files untouched. No network, APT transactions, commits, pushes or merges.

Resolve the remaining source-backed questions autonomously, and report any genuine security or scope blockers. Do not recommend `cdots apply git` until non-installing validation is ready.

All three tarball SHA256 values match their `.dsc` declarations; no signature authentication is claimed. Reading `configuration.cc:898–949` and `strutl.cc:292–333,388–403` establishes the bounded comment subset and shows why non-comment percent syntax cannot be passed through the directive scan. Dump `%v` emits raw values, so the observed backslashes are literal, not escape syntax.

### L7 — Verified parser slice and remaining execution contract
Focused RED exited 1; focused GREEN, full eight-package offline suite, vet and diff/format checks exited 0. The APT profile dump now parses with all observed regex bytes intact and still fails closed at execution configuration validation. Neither executable redirects nor additional hooks were admitted. No native tests or transactions ran.

APT `init.cc:159–161` sets methods and the solver/planner list defaults; matching these observed defaults in a snapshot would not by itself authorize external solver/planner dispatch or establish executable ownership. `private-cmndline.cc:548–556` moves Binary scope before CLI parsing; `dpkgpm.cc:405–525` runs hooks in list order, with protocol and FD selected per first command token; errors stop subsequent hooks. `apt-config.cc:67–74` allows configurable dump formatting and omission of empty values, so snapshot format/completeness must also be pinned before relying on it. Preserve format/privacy guards, first-hook enforcement, per-hook options and drift rejection.

`apt-listchanges.py:57–85,160–171` can attach a terminal, send email, and write a seen database. `work/dpkg-preconfigure:113–124` consumes filenames on stdin and reopens `/dev/tty`. Do not admit `apt-listchanges` based on its wrapper/protocol alone; verify its trusted executable/config/effective frontend and side effects under the exact contract. Separate non-installing proof must be reviewed before a VM run. Native assessment still reports undeclared untracked scope, and independent verification was not performed.

### L8 — Current request
Continue in `/home/ace/Proyectos/cdots-apply-absence-fix`. Keep original main untouched.

The source-backed APT 2.6.1 parser corrections and offline validation are accepted as partial progress, not execution-security approval or VM retest readiness.

Goal: complete the remaining Debian 12 root-execution compatibility checks while preserving the agreed fail-closed security contract.

Focus on the remaining blockers: deterministic `apt-config dump` format, legitimate Debian executable-path defaults and trust boundaries, effective `apt-get -c` configuration and Binary scoping, configuration drift, and the exact apt-listchanges protocol/FD contract, including potential mail and seen-state side effects. Use the verified local sources and VM evidence. Preserve the existing comment and escape fixes.

I explicitly authorize **one scoped `gentle-ai-verify` worker** for independent, read-only verification of the relevant security changes (Risk: item 2). This overrides the previous no-new-worker restriction only for that verifier. No other workers are authorized. Do not treat passing tests as a substitute for independent verification.

For the native assessment scope blocker, select only related untracked tests and fixtures if supported. Do not include, modify or revert unrelated files, including the existing skills-lock.json change. If the assessment cannot isolate the intended scope, report the limitation instead of expanding it.

Continue autonomously with bounded implementation and offline tests. Escalate only genuine security decisions or blockers that cannot be resolved safely.

Constraints: no VM/APT transactions, network, commits, pushes, merges, main changes, unrelated workspace modifications or historical S24 work.

Report changes, independent verifier findings, test results, remaining risks and readiness for a non-installing Debian 12 validation. Do not recommend `cdots apply git` until the required security evidence is complete.

### L9 — Pinned dump and independent findings
Apply now pins `%F "%V"` framing and includes empty values, and validates canonical value encodings. The retained debconf hook requires exactly protocol 1/FD 0 in effective evidence. Focused RED then GREEN, full offline suite, vet, format and diff checks passed on that slice. Native assessment cannot isolate tracked `skills-lock.json` via untracked selection; no scope change was made.

The one authorized verifier (mwu6evkh-1-01wk) independently reviewed source and reported two static blockers: the hash-comment rule did not account for block-comment context, and raw noncanonical dump keys could pass. It ran no tests: HEAD did not contain the candidate and no pinned replacement was supplied. The focused regressions reproduced both validation gaps with inert inputs (exit 1). Parent changes reject block-comment openers in active text and enforce canonical raw keys; post-fix focused tests, the full eight-package Go suite, vet and diff check exited 0 (job-5). The verifier did not review these follow-up changes. No second verifier was launched.
