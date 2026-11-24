# Guarded disposable APT fixtures — T1 only

**Current outcome: [BLOCKED — D1 writer task-registry incident](RESULTS.md#d1-offline-integration-task-registry-incident).** D1 design was accepted and conditional offline/inert work authorized. Parent integrity21/proc3 focused tests passed, but both new writer IDs disappeared from the registry; kernel remains RED/module missing and mount final capture files are empty, not GREEN. Implementation/combinedsuite/sourceaudit/live isolation are unverified. Preserve artifacts; no competing relaunch or live work.

This stdlib-only Python 3.11+ harness implements safety gates and bounded PTY fixtures, **not real-install evidence**. The parent's latest inert probe started one container and collected the fixed read-only probes, then rejected its running inspect environment; owned cleanup succeeded and the parent confirmed zero containers. Full running-profile acceptance and package fixture results are not established. Writer tests remain offline; further live probes are parent-routed. Production code and managed documentation are unchanged.

## Correction-module status

`mount_checks.py` requires full-profile boundaries and fresh runtime/inspect bindings, but deliberately refuses unresolved overlay aliases including the retained `../diff1` form. `host_integrity.py` snapshots selected stable readable package state, explicitly excluding locks/cache/credentials; it does not attest the entire host, symlink targets or restored intermediate changes. Neither module is integrated into `runner.py` yet. Sanitized running/mount fixtures derive from the prior failed probe, not an isolation success.

Separate worker checks (reported exit 0; not a combined suite):

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s test/e2e/apt -p test_mount_checks.py -v
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s test/e2e/apt -p test_host_integrity.py -v
```

No further live probe is permitted while physical layer provenance and independent corrected-design verification remain unresolved. The bounded metadata permission does not authorize bypassing OS access checks or escalating execution privileges.

## Safe offline checks

From the repository root:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -B -m unittest discover -s test/e2e/apt -p 'test_runner.py' -v
PYTHONDONTWRITEBYTECODE=1 python3 -B test/e2e/apt/runner.py
```

The second command must exit 2 without calling Podman, building, or invoking package tools. The suite installs a fail-closed `runner.subprocess.Popen` guard at import: only four exact harmless Python fixture argv sequences may spawn. Absolute Podman/Go/APT/cdots paths and arbitrary Python scripts are rejected before the underlying spawn. Synthetic executors match complete argv and reject unexpected commands. Stop/cleanup regressions exercise the actual methods, not mocked stop callbacks.

Containment was added after stale subprocess mocks attempted Podman logs/stop against a synthetic nonexistent ID. Parent reported nonzero results and no containers; no successful mutation was established. That incident is not live fixture or isolation evidence.

## Inert probe mode (parent-routed only)

After independent inspection, the parent may route this diagnostic mode again; the writer has not run it:

```sh
python3 -B test/e2e/apt/runner.py --probe-only
```

`--probe-only` and `--run-disposable` are mutually exclusive. Probe-only accepts no scenarios: it inspects the pinned image, creates the fixed inert sleep container, records raw created-container inspect and prestart mapping evidence, applies the unchanged strict prestart gates, and starts **only if those gates pass**. It then collects namespace/mapping/mountinfo, root/security and OS probes. There is no build, copy, APT command (not even `--version`), dpkg/package query, metadata refresh or cdots/apply path in this mode. It does not take host package-database hashes or claim package invariance. The complete fixed namespace/maps/status/mountinfo/OS probe set is collected and saved before its validators run, so a guest-map mismatch retains the later read-only data too. Collection is not authorization: package/copy paths remain blocked unless every validator passes. Command failures can still leave partial evidence. Owned cleanup runs on rejection. Unknown fields/defaults remain blockers, not permission to broaden schemas.

## Future apply opt-in (not authorized by this writer task)

After independent safety inspection and explicit authorization, the minimal install/no-op selection is:

```sh
python3 -B test/e2e/apt/runner.py --run-disposable --scenario install --scenario no-op
```

Requirements: non-root Linux host, Python 3.11+, `/usr/bin/podman`, rootless subordinate UID/GID ranges, local Go compiler at `/usr/local/go/bin/go` or `/usr/bin/go`, cached Go toolchain, and the exact cached Linux amd64 image below. No pulls, installations, elevation, or fallback to host `apply` are implemented. The future verified containers need network access for `apt-get update` and package downloads. Build uses `CGO_ENABLED=0`, Linux/amd64, local toolchain and offline module settings; build outputs/cache stay in private evidence.

| Pin | Exact value |
| --- | --- |
| Local image ID | `94926f0fd968cd68ac486502185a5dd78687874a9ec7727f64fe2af696e28311` |
| Image digest | `sha256:2c037a04925515fdd6ea85ea14a682d0e79931f5e9f5d07b6dbfc6ba12f9e858` |
| Required guest | Debian 12/bookworm, amd64, APT 2.6.1 |
| Container creation | `--pull=never`, `--http-proxy=false`, `--network=slirp4netns:allow_host_loopback=false`; `/usr/bin/podman --remote=false`; private empty `--hooks-dir` |

`--scenario` is repeatable with unique choices; `--timeout` is 1–600 seconds per command (default 180). Exact build, create, probe, copy, package, and apply commands are constructed in `runner.py` and retained with return codes. There is no arbitrary command/image/package flag. The application catalog is not changed; fixtures request only `git`. Before/after manifests query every package recorded by dpkg, including dependencies.

## Review gates before execution

1. Host UID and effective UID must be nonzero; remote/config/loader override environment is refused. Runtime must report local/rootless. Inspect the pinned local image and reject any changed configuration, volumes, or entrypoint.
2. Create only a unique labeled container. Before start, validate full ID, exact name/image/labels, private namespace modes, subordinate mapping declarations translated through `podman unshare`, no mounts/volumes/devices/published ports/pod/rootfs/dependencies, and conservative capabilities/security.
3. After start, perform fixed read-only probes: guest UID/GID maps match the parent-relative declarations, and composition through freshly read parent mappings matches host-visible init maps; guest root is subordinate-mapped; user/mount/PID/network/IPC/UTS/cgroup namespaces differ from host; `NoNewPrivs=1`, seccomp filtering, conservative mountinfo, Debian/bookworm and exact APT version. **No package operation or copy is permitted before these complete.** The APT version probe is read-only and last in the gate.
4. Copy the built binary using `podman cp`, never a mount, and independently compare its SHA-256. Only then refresh container metadata and exercise a fixture.
5. PTY fixtures connect all three streams to a real PTY and use `podman exec -it`. Literal `apply`, `deny`, EOF and native `y` responses are sent only after exact expected prompts. Native APT approval is separate and follows application confirmation. **No indirect review/removal operations are enumerated for approval:** any such prompt is refused, never parsed into an answer.

### Fail-closed compatibility warning

The first inert probe supplied actual **created-state Podman 4.9.3 inspect and parent userns maps**; the latest probe also supplied running inspect and read-only guest evidence, but that full profile is not yet accepted. `fixtures/podman-4.9.3-created.json` retains that observed shape with deterministic all-a container ID, all-b ownership token, `/synthetic-evidence` cidfile/hooks coordinates and `/fixture` storage/runtime paths; session identities and timestamps are removed. The historical captured argv retains plain `slirp4netns`. New creation explicitly disables host loopback, and the runtime gate additionally requires inspect's create declaration to match the exact newly issued argv; a mode-only `slirp4netns` field cannot authorize an inconsistent declaration.

The narrow compatibility parser checks consequential values, not merely field-name unions: private `CgroupMode`, systemd/default cgroups, exact journald/restart/resource defaults, pinned digest, owned cidfile annotation and path, the minimal environment, conservative network/capability surfaces, and bounded map strings. Empty `UsernsMode` is **not** evidence of privacy: it requires declared maps translated through fresh parent mappings before start, followed by actual distinct user/other namespaces and host-visible subordinate UID/GID probes after start. The observed `0:1:65536` declaration with parent `0 1000 1` / `1 100000 65536` resolves to host IDs 100000–165535. Overlap, aliases, malformed/overflowed declarations and host-root/user ranges are rejected.

**Full runtime/isolation remains unverified.** The bounded mapping increment replaces incorrect direct guest/host equality with declaration and fresh-parent composition checks. Observed guest `0 1 65536` and host-init `0 100000 65536` are different coordinate frames, not contradictory maps. Offline regressions reject mismatched declarations, altered parent ranges, aliasing, overflow and host-root/user mappings, and verify a poststart parent re-read before authorization. The running environment/schema/owned-path and exact mount-tuple adaptation, plus complete sanitized running/probe fixtures, remain pending; mountinfo acceptance is unchanged. No full-profile GREEN or independent acceptance is claimed. Further source-evidenced changes require independent inspection, not deleted checks or speculative acceptance. Private runtime reports are evidence inputs, not native-review candidate trees. Passing offline tests establishes neither live isolation nor installation.

## Scenario behavior

| Selection | Intended observation / limitation |
| --- | --- |
| `install` | Install git only if absent; successful result plus independent installed post-state. Never remove a preinstalled package to force it. |
| `no-op` | Git must already be installed; no confirmation/execution and unchanged queried state. May share the immediately preceding successful install container. |
| `wrong-approval` | Literal `deny` at final application confirmation; cancellation, no execution, unchanged queried state. |
| `eof` | EOF at exact final application confirmation; cancellation before execution. No SIGINT scenario is claimed. |
| `non-tty` | No PTY; assert genuine-terminal refusal and no execution. |
| `native-config` | Fresh container, copy benign `debug 0` into a new dpkg config part; assert production native-config rejection without approval or execution. No protection flag is disabled. |
| `apt-error` | Fresh container, copy one malformed APT sources-list fixture; assert real APT parse-error rejection with no execution. No fake APT binary. This is not an executed partial-failure test. |
| `protected` | Explicitly unsupported: desired-present catalog cannot express protected removals. No gate bypass or fabricated result. |

Every denied/error fixture gets a fresh independent container. Git already present makes install/denied/error fixtures unsupported. A pre-execution failure is unsupported only when its unique anchored result records and a literal diagnostic match the enumerated capability table in `runner.py` (currently the source-established APT-version diagnostic). Unknown production failures are failure/unclassified, never unsupported or passed. Duplicate, contradictory or unknown result records fail closed; transcript remains authoritative. Unexpected review/removal prompts fail safely. Upgrades, removals, drift, native cancellation, deliberately executed partial failures, SIGINT, and arbitrary dependency approvals are not validated by this slice.

## Evidence and cleanup

Each opt-in invocation allocates `test/e2e/apt/evidence/cdots-phase5a-*` inside this worktree with mode 0700 and independent owned subdirectories. The evidence root must be an owned mode-0700 directory, not a symlink; otherwise the runner refuses without changing an existing directory's permissions. Ignore inherited `TMPDIR`. Evidence is saved before owned container removal, remains available afterward, and is excluded from the source review candidate. Do not publish raw local paths or runtime records without a privacy review. Retain:

- Detailed argv, return code, stdout/stderr and uncertainty logs; raw inspect and partial/complete read-only probe evidence. All ordinary command captures (including build/inspect/update/logs/non-TTY) drain both streams incrementally with a combined 4 MiB cap; overflow/timeouts are explicit failures, not unlimited whole-output captures. PTY transcripts are separately bounded.
- Per-scenario PTY transcripts and complete before/after `dpkg-query` package/version/status manifests, including dependencies. Logs preserve failure transcripts too. Emergency stopping can prevent a post-state query; missing post-state is not success. Package manifests do not prove all maintainer-script, filesystem, service or network side effects.
- In apply mode, before/after SHA-256 inventories of regular files under host `/var/lib/dpkg`, `/var/lib/apt`, `/var/cache/apt`, without invoking host package tools; assert equality only with complete evidence. Traversal, inventory, permission and disappearing-file errors fail closed; symlinks are inventoried, not followed. `/etc/apt` is intentionally not read because it may contain credentials. Transient changes restored between snapshots are not detected. Concurrent changes are unclassified; attribution is unknown.
- `summary.json`: passed/unsupported/failure/uncertain, individual observed outcomes, host equality and cleanup errors. An unsupported or uncertain invocation exits nonzero.

Timeout and non-TTY overflow request stop of the exact owned container and verify stopped state through a critical path that does not depend on any log-storage success. Full ID/name/image/labels are freshly validated, even with broken storage. Local command/PTY clients are terminated on failure. Cleanup attempts inspect/log retention before removal and attempts safe stop in `finally` even if retention fails. It confirms the exact owned object is stopped and never removes an unverified/running object; failed evidence retention leaves the stopped object retained and reports uncertainty. Removal has no force or volume removal. No prune or unrelated-object cleanup exists. If ownership cannot be proved or runtime stop/inspect fails, report uncertain state and retain the identifier for independent follow-up; never guess a target. Forced interpreter/host termination cannot guarantee cleanup.

## Next step

Parent routes independent safety inspection, then separately authorizes T2 live execution if the gates are accepted. T1 does not update the ODD task journal, run broad Go checks, install tools, or perform Git delivery actions. Phase 5B has no functionality dependency on this harness.
