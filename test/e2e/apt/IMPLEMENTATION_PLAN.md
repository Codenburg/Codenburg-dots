# D1 offline integration contract
Spec: odd/tasks/phase5a-apt-e2e.md, T1, S17. D1 design accepted; implementation not yet accepted.
No live metadata/proc/package/storage reads or container commands during offline integration.

## Parallel units and interfaces
- A: new isolation_checks.py and test_isolation_checks.py: PURE kernel-evidence validation, no IO/process execution.
- B: mount_checks.py, test_mount_checks.py and an explicitly synthetic controller-mountinfo fixture: strict full mount classification/correlation.
- Parent: proc_evidence.py/test_proc_evidence.py (fixed ordinary proc IO/schema, synthetic fixtures), runner.py/test_runner.py, host_integrity.py/test_host_integrity.py, documentation and tracker. No overlap with A/B.
Proc IO is separate from pure A validation for bounded reviewability; collect_host(pid) MUST follow owned native inspection and never exposes arbitrary PID selection to CLI.
Target <=400 diff lines per review unit; report before an oversized/broadened change, do not dilute security checks to meet budget.

A public API:
`validate_kernel_bundle(host, guest, *, runtime_info, before_inspect, after_inspect, uid, gid, groups)`.
Raises `IsolationEvidenceError(ValueError)` on missing/contradictory/unsupported evidence; returns a JSON-serializable narrowly labelled kernel-check report, NOT global isolation PASS.
Host: {controller: frame, init: frame, init_stat_after: raw_stat}.
Guest: {init: frame, collector: frame}.
Frame fields: stat/status/uid_map/gid_map/mountinfo (raw strings), namespaces (dict of the seven raw ns-link values).
Controller/init/collector PIDs come from stat/status; init PIDs differ by host/guest coordinate, matching start-time plus NSpid and namespace object IDs binds them.
Validate all D1 G3-G5 requirements, including restart/PID drift, strict capability masks, map coordinate composition and root/caller/group exclusions. No imported runner or circular dependencies.
Runner brackets proc collection with owned native inspect and preserves raw evidence. Parent owns fixed guest collector + host ordinary proc IO. A may inspect runner's existing declared/runtime-map shapes but cannot edit runner.

B extends existing `validate_mountinfo` with keyword `controller_mountinfo=None`.
Mandatory for require_full=True. Preserve non-full helper behavior, never use it as live isolation approval.
Host-init mountinfo is primary. Controller mountinfo translates exact current-inspect generated-file paths into filesystem root/device coordinates; no basename/prefix shortcuts.
Accept opaque supported overlay aliases as diagnostic under D1 trust, do not resolve/normalize them or demand equal lowerdir counts. Preserve all genuine mount/mask/flags/parent/unknown-share requirements.
Return None for accepted FULL mount evidence or raise existing MountValidationError. Not overall isolation proof.
Parent separately correlates init/collector/host namespace and semantic mount data; no guest-only fallback.

## Parent integration and verification order
Use existing explicit probe-only mode; no apt/build/copy/scenario path in granted run. Keep default CLI refusal and existing apply-mode behavior regression coverage.
Separate D1 created/running profile from unapproved future APT profile: capALLdrop, nomap, networknone, NNP/seccomp,128MiB,1CPU,64PIDs, stable minimal env/hostname and TERM-handling fixed shell.
Host integrity API must offer explicit optional_files exclusion (empty tuple) with fail-closed unsupported selections; preserve existing default helper semantics offline, but D1 NEVER reads optional paths.
Capture selected baseline before create and post-owned-cleanup comparison; no broad host_unchanged claim.
Freeze exact final argv/collector/schema/readscope and budgets in docs, then focused RED/GREEN, current combined Python suite with denyguard and owned worktree TMPDIR.
Independent source audit BEFORE any live execution. Native risk assessment/review if applicable remains separate; no commits.
Only after all gates pass: exactly one inert container, <=30s/capture,4MiB output,600s total inclusive cleanup; no retries or relaxed limits. Missing evidence stops and cleanup is owned only.
