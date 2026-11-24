# Phase 5A — Fresh inert probe: BLOCKED

**Latest checkpoint:** [D1 offline integration is BLOCKED on a task-registry incident](#d1-offline-integration-task-registry-incident). The human granted conditional integration/one inert validation, but the required combined suite/source audit have not passed. Design acceptance and prior metadata evidence remain preserved; no new live validation has run.

The authorized recovery ran one real rootless Podman probe, not a real `cdots apply` transaction. The harness refused its running environment. Do not treat observed namespace or mapping data as complete isolation acceptance.

## Recorded execution

| Check | Actual result |
| --- | --- |
| Current offline Python suite | 52 tests passed, exit 0; mocked coverage, not installation evidence |
| Default runner | Exit 2; empty stdout; stderr `Refused: choose an explicit disposable mode (review required).` |
| Runtime | Podman 4.9.3, local/rootless, host UID/EUID 1000 |
| Fresh inert probe | Exit 1: `unexpected container environment` |
| Inert container start | Exit 0; prestart gates and exact issued create declaration matched |
| Read-only collection | Complete fixed probe set retained; downstream validators not reached |
| Owned lifecycle | Logs, stop and removal exit 0; stopped state independently inspected, PID 0 |
| Final container listing | `[]`, exit 0 |
| Host integrity inventories | Incomplete before and after: `PermissionError` on `/var/lib/dpkg/lock` |
| Bound source identity | Five reviewed file hashes/modes unchanged during execution |

Recorded commands, from the worktree root:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -B -m unittest discover -s test/e2e/apt -p test_runner.py -v
python3 -B test/e2e/apt/runner.py
/usr/bin/podman --remote=false info --format=json
/usr/bin/podman --remote=false ps -a --format=json
PYTHONDONTWRITEBYTECODE=1 python3 -B test/e2e/apt/runner.py --probe-only --timeout 180
```

These are reproduction instructions, not authorization for another probe. No retry or gate relaxation was performed. Future execution requires a fresh human decision and applicable safety review.

## Isolation observations and blockers

- The pinned cached Bookworm image ran only `/bin/sleep infinity`; no privileged mode or declared mounts/devices were added.
- Seven guest namespace identities differed from host. UID/GID declaration and guest maps were `0:1:65536`; host-init maps were `0 100000 65536`, through parent maps `0 1000 1` and `1 100000 65536`.
- Inspect reported `Privileged=false` and `Mounts=[]`. Guest status reported `NoNewPrivs=1`, `Seccomp=2`, zero inherited/ambient capabilities and bounded permitted/effective capabilities.
- Running inspect added generated `HOME=/root` and `HOSTNAME=<CID prefix>`, which the current environment gate rejects. This is a harness compatibility refusal, not a confirmed Phase 4 defect or isolation escape.
- Running schema, mapping, security, mount and OS validators after that refusal were not reached. Mountinfo contains generated files, device mounts and proc masks whose complete boundary validation remains outstanding. A declared empty mounts list alone does not prove absence of sensitive sharing.
- Missing host hashes forbid a `host_unchanged` claim. No permission escalation or protection bypass was used.
- Native Podman stop warned that SIGTERM did not stop the inert sleep process within two seconds and resorted to SIGKILL. Stopped state and removal were confirmed. This is not evidence of `cdots apply` cancellation behavior.

## Retained local evidence

Evidence directories are UID1000-owned, non-symlink, mode 0700 and Git-ignored:

- `evidence/execution-recovery-1251e2877c9748e9b47a8ca7c2e97d8e/`: exact argv, return codes, output, current offline-suite result, before/after source identities, host-integrity failures, retained-record manifest and `execution-verdict.json`.
- `evidence/cdots-phase5a-cd5xiehq/`: raw created/running inspect, fixed probes, all 30 runner command records, final inspect and `summary.json`.

Raw inspect/probes were persisted before owned container removal. Records remain in the worktree afterward, separately from mocked tests and source review. Do not publish raw records without a privacy review; ignored local evidence will not accompany a future Git commit automatically.

## Unexecuted scenarios and remaining gates

No build, binary transfer, APT version probe, metadata refresh, dpkg query or real `cdots apply` scenario occurred in this recovery. Install, no-op, wrong approval, EOF, non-TTY refusal, native-config rejection and APT-error fixtures are all unexecuted—not passed.

Protected removals cannot be expressed by the current desired-present catalog. Upgrades, removals, drift, SIGINT/native cancellation, deliberately executed partial failures and arbitrary dependency approvals remain outside this harness's validated coverage.

Historical Phase 4 Go tests, vet and Linux build passed; the historical race batch timed out initially and a later recorded run passed. Completed Go work was not restarted. These historical checks do not replace real-install evidence. Independent current source audit accepted only the inert diagnostic scope, not full isolation or APT execution. Native review has not started; functional/isolation gates remain unmet, and no native approval is claimed.

## Git, ownership and next decision

Branch remains `feat/phase5-apt-e2e`, HEAD `23a6e9ee423fd22eb95da64fa0bee63dee59614c`. Tracked/staged diffs are empty. Harness files, scoped documentation and the task journal remain untracked; no commit, push or merge occurred. Production code, managed documents, provider selection and Phase 5B were not changed. No Phase 5B production integration dependency has been identified.

**Needs your decision:** authorize bounded running-profile/mount validation and host-integrity evidence work followed by a newly reviewed probe, or retain this blocked result. Do not run mutation-capable scenarios until isolation is fully accepted.

## Correction follow-up: BLOCKED before another probe

The human authorized bounded harness corrections, then explicitly authorized metadata-only `lstat`/`readlink`/`realpath` under `/home/ace/.local/share/containers/storage/overlay`, restricted to the pinned image and Phase5A-owned layer references. No file contents, tree scans, mounts, package commands or runtime-storage writes were authorized by that metadata decision.

| New check/work | Actual result and scope |
| --- | --- |
| Selected-integrity module | Worker completed `host_integrity.py` and 20 offline regressions; final exit 0. Original helper RED reproduced unreadable dpkg-lock failure. Not integrated into runner. |
| Strict mount module | Worker completed `mount_checks.py` and 14 offline regressions; final exit 0 after one assertion-message correction. Original helper RED rejected `/run/.containerenv`. Not integrated into runner. |
| Actual retained mount profile | Still refused: `overlay lowerdir alias provenance/topology unresolved; physical storage reads not authorized`. Canonical positive fixture establishes lexical behavior, not physical isolation. |
| Authorized metadata-only lookup | Exit 1: `[Errno 13] Permission denied: '/home/ace/.local/share/containers/storage/overlay/b4425c7e4123b9d9b50fa18d1c83e180a18085cef812d042cb200d977bf3c733/diff'`. Overlay-root metadata accessible; layer-directory provenance unresolved. |
| Combined current Python suite | Not run; separate 14/20 worker GREEN and historical 52-runner GREEN are not an integrated suite result. |
| Parent environment/profile integration | Unfinished. Generated HOME/HOSTNAME and other running-profile differences were diagnosed from retained inspect but not accepted by a corrected runner. |
| Independent corrected-design verification | Not run; incomplete provenance prevents a complete accepted design. |
| Native assessment/review | Read-only assessment unassessable because untracked scope lacks explicit declaration; conservative high-risk independent-verifier plan returned. No native review START, approval or acknowledgement. |
| New inert probe / real APT | Neither executed. No build, copy, refresh, package query or apply execution in this follow-up. |

New private evidence: `evidence/cdots-phase5a-n7v_ea3h/overlay-metadata.json` contains the exact scope, accessible root metadata and permission failure; it explicitly associates the request with retained owned-container inspect, not a fresh container. `evidence/host-integrity-unit/results.md` retains the host-module RED/GREEN output/self-review; the mount-module exact output is in its worker transcript. Existing raw recovery records remain untouched. Do not publish private evidence without privacy review.

The selected-integrity module deliberately excludes locks, caches, credentials and unselected paths. Symlink targets and restored intermediate changes are not attested. No whole-host `host_unchanged` claim is made, and this module has not yet replaced the runner's old integrity implementation.

For the inert cleanup investigation, [Linux PID namespace documentation](https://man7.org/linux/man-pages/man7/pid_namespaces.7.html) states that namespace init accepts ordinary signals only with installed handlers, while SIGKILL from an ancestor is exceptional. [Podman 4.9.3 stop documentation](https://docs.podman.io/en/v4.9.3/markdown/podman-stop.1.html) specifies SIGTERM followed by SIGKILL after the chosen timeout. This is consistent with the retained inert `/bin/sleep infinity` PID1 timeout under `stop --time=2`; the original PID1 handler state was not captured, so this remains a supported explanation rather than direct handler-state proof. It is not application cancellation evidence. Existing ownership/stopped/removal guards remain unchanged.

Phase5B explicitly released the coordination window after reporting its race run complete; no heavy jobs were reported remaining there. Those are peer reports, not Phase5A test results, and its separate verifier-HOME blocker is not a Phase5A production dependency. No further race run or peer-worktree access was requested.

Following the user's instruction, **“If isolation cannot be established, stop with the precise blocker.”** The direct metadata lookup was not retried through a more privileged execution context. No permission escalation, isolation bypass, further probe/APT execution, commit, push or merge was attempted.

**Needs your decision:** retain this BLOCKED outcome, or decide on a separately reviewed, metadata-only runtime-managed provenance strategy within the original safety constraints. Parent environment/profile correction and integration, the current combined Python suite, independent design verification and applicable native gates remain pending. A new inert probe still requires accepted design; real APT still requires complete accepted isolation.

## Podman-managed metadata investigation: BLOCKED on first-command timeout

S14 authorized only read-only `podman info`, pinned `podman image inspect`, and exact Phase5A-owned `podman container inspect`. It expressly prohibited direct protected-overlay access, escalation, `podman unshare`, permission/storage changes, container creation, isolation probes and APT during the investigation. The previous direct-directory grant was not used.

Exactly one command was attempted:

```text
/usr/bin/podman --remote=false --hooks-dir=<private-evidence>/empty-hooks info --format=json
```

| Observation | Actual result |
| --- | --- |
| Harness command bound | 5 seconds; reached |
| Native return code | Unknown (`rc: null`), `state: uncertain-timeout` |
| Captured stdout / stderr | Both empty |
| Enclosing foreground capture | Exit 1, elapsed 5.2 seconds; not the outer shell's 30-second timeout |
| Pinned image inspect | Not executed; loop stopped on first-command failure |
| Exact historically owned CID inspect | Not executed |
| New provenance strategy | Not established |
| Conditional focused regressions | Not applicable: prerequisite sound strategy absent; existing tests/gates preserved |
| Independent strategy verification | Not run: no complete strategy proposed, no subsequent execution |
| New isolation probe / real E2E | Neither executed |

Private evidence remains in `evidence/cdots-phase5a-jeo3icou/`: `command-0001.json` preserves exact argv, null native return code, timeout state and empty output; `metadata-manifest.json` preserves the incomplete capture; `investigation-verdict.json` distinguishes the attempted timed-out command from the manifest's empty successful-command array. No original evidence was overwritten.

**Exact evidence gap:** no current runtime JSON or new image/container metadata was captured. The protected layer's physical ownership, overlay alias resolution and correspondence to actual live mounted layers remain unverified. GraphDriver paths are not accepted as filesystem ownership or isolation proof. This timeout does **not** show that the permitted APIs are intrinsically sufficient or insufficient; their substantive evaluation remains incomplete. The timeout cause was not diagnosed or attributed to Phase5B. Its resource window remains released.

No automatic retry, restart, background migration, execution-context change or wider filesystem investigation followed. No direct overlay, unshare, privilege/permission/storage mutation, container-create/probe/APT/build or Git-delivery command was issued. No harness/test source changed, no existing completed tests were restarted, and no native review was started for this investigation. Context7 resolved a Podman documentation library, but no substantive documentation query or sufficiency challenge proceeded after the failed capture.

The user's stop condition applies: **“If reliable provenance cannot be established, remain BLOCKED and report the exact evidence gap.”** T1 is blocked; T2/T3 remain pending; reporting is complete. Existing environment/profile integration, combined current Python suite, independent design approval and applicable native gates remain incomplete from the previous correction work.

**Needs your decision:** whether to authorize a separate managed-metadata attempt with a reviewed command ceiling, or retain BLOCKED. Nothing will be retried automatically; a future attempt would not by itself authorize probes or APT. No reliable provenance/isolation or READY_TO_INTEGRATE claim is made.

## Thirty-second managed-metadata attempt: capture compliant, provenance BLOCKED

The human authorized one new metadata attempt with a 30-second limit per command, preserving the prior environment, empty hooks and read-only scope. This was a separately authorized attempt, not an automatic retry or a default-timeout change. No source, test, or safety gate changed.

All commands used `/usr/bin/podman --remote=false --hooks-dir=<new-private-evidence>/empty-hooks`:

| Command suffix | Natural native exit | Recorded duration | stdout | stderr |
| --- | ---: | ---: | --- | --- |
| `info --format=json` | 0 | 5.212949 s | Complete runtime JSON | Empty |
| `image inspect <pinned-image-ID>` | 0 | 0.140229 s | Complete one-image JSON array | Empty |
| `container inspect <removed-owned-CID>` | 125 | 0.131153 s | Exactly `[]` followed by newline | `Error: no such container <removed-owned-CID>` followed by newline |

The enclosing capture completed with exit **0** in about six seconds. Every timed record has `deadline_seconds: 30`, `state: completed`, `timeout_triggered: false`, equal natural/final return codes, and an empty local capture-group cleanup array. No timeout, signal cleanup, or repeat occurred. Wrapper success means the capture sequence finished; it does **not** turn native exit 125 into a successful container inspection.

The restricted environment was unchanged: `HOME=/home/ace`, `XDG_RUNTIME_DIR=/run/user/1000`, `PATH=/usr/bin:/bin`, `LANG=C`, `LC_ALL=C`. The private hooks directory was empty. Recorded runner SHA256 before/after matched; this is recorded source equality, not a fresh independent hash, HEAD verification, or repository-wide mutation audit. The independent assessor corroborated the retained command/timing/stream records against the decoded metadata.

Private retained evidence: `evidence/cdots-phase5a-bqe3_j1j/metadata-attempt.json`, `timed-{runtime,image,owned-container}.json`, `command-0001.json`–`0003.json`, `runtime-metadata.json`, `image-metadata.json`, `independent-assessment.txt` and `provenance-verdict.json`. Exact raw stdout/stderr and argv remain private there; do not publish without privacy review. Older evidence was not overwritten.

### Independent sufficiency assessment

Read-only `gentle-ai-verify` task `mv5s50o5-5-m5qv` returned **BLOCKED — metadata capture compliant; provenance insufficient**. It read repository/evidence only: no live command, scratch HOME, test, build, runtime query or further experiment. S14 capture-scope and S15 recorded-attempt compliance passed; overall physical provenance/isolation did not.

The current metadata declares local/rootless Podman 4.9.3, linux/amd64, the expected parent UID/GID maps, overlay store configuration with zero registered containers, and the pinned image ID/digest with one RootFS layer `6439a9e3…`. These are useful identity/configuration observations, not proof of actual filesystem ownership, resolved alias topology, or effective isolation.

**Exact missing evidence:**

1. A trustworthy binding from image layer identity to actual backing objects, ownership and absence of redirection outside the intended store.
2. Causal provenance/association for the extra `b4425c7e…` layer in the retained container LowerDir. It may be legitimate runtime-generated data; the gap is not evidence of malicious behavior or a production defect.
3. Ordered resolution of retained aliases `FHTCR7…`, `FHTCR7…/../diff1`, and `4E6RQF…`, reconciling three kernel lowerdir entries with two inspect entries. Lexical normalization and path-prefix matching do not provide that proof.
4. Current selected-container PID, namespace, effective mounts/security/maps and mounted-layer binding. The selected CID is absent; the old running snapshot is stale. Runtime-wide mappings do not attest a particular current container.
5. A new host-integrity comparison and complete effective isolation validation.

The previous protected-directory PermissionError provided no missing resolution proof. GraphDriver strings alone remain insufficient and the conservative alias guard remains intact. No sound provenance strategy was established, so conditional new regression coverage did not activate; historical 52/14/20 checks remain preserved, not current integrated-suite proof.

The new info capture took slightly longer than five seconds. This is consistent with the prior ceiling being too short for that observed successful call, but does not diagnose the earlier timeout or prove a hang, storage defect, or isolation failure. Since this new attempt did not time out, the requested second-timeout diagnosis branch did not trigger. No further runtime call followed.

### Outcome and next gate

**BLOCKED**, applying the existing stop condition: **“If reliable provenance cannot be established, remain BLOCKED and report the exact evidence gap.”** No direct overlay access, unshare, privilege escalation, permission/storage-mutation, create/start/probe/APT/build or Git-delivery command was issued. Phase5B's window remains released; it is not a blocker or an attributed timeout cause.

**Needs your decision:** retain BLOCKED, or separately approve consideration of a concrete evidence mechanism capable of supplying those missing bindings within the existing safety constraints. Successful metadata capture is not probe/APT authorization. A validated provenance mechanism and independent corrected-design verification are the next required gates; environment/profile integration, combined Python checks, effective isolation, real E2E and applicable native review remain outstanding. No READY_TO_INTEGRATE or complete-Phase5 claim is made.

## Boundary-based design D1 accepted; execution BLOCKED

Independent assessor `mv5u41uh-7-80cp` accepted [ISOLATION_DESIGN.md](ISOLATION_DESIGN.md), G1–G10, with no design blockers. Physical alias ownership/resolution is not a necessary standalone gate under the explicit trusted local runtime/kernel/operator/storage assumptions; mandatory current kernel boundary checks and all genuine no-host-share/identity/privilege guarantees remain. GraphDriver path strings still do not prove filesystem ownership.

This is **design acceptance only**, not implemented-validator approval, proc-access feasibility, current isolation, probe authority, APT permission or E2E PASS. The reviewer used repository/evidence reads only, no tests/commands/live reads/writes. Prose structural check passed; executable tests were appropriately deferred until validator changes exist. Historical checks remain preserved, not rerun or recast as integrated proof.

Next required grant: offline integration with exact argv/schema/collector/read scope/resource and cleanup limits frozen; current combined offline tests and independent source audit must pass before exactly one inert validation. Tests must additionally reject PID reuse, missing independent host proc despite matching guest text, wrong implicit-file filesystem/device binding and integrity reads outside permission. Optional host-integrity paths need explicit authorization or exclusion.

Future ordinary owned-PID proc access remains unverified. If required evidence is inaccessible or the trust boundary unacceptable, use the separately reviewed disposable Debian12 VM/test-host alternative—no privilege escalation, guest-only fallback, storage access or guard relaxation. Eventual APT networking/capability/trust requirements remain a separate decision.

T5 design complete; T1 externally blocked on the next grant, T2/T3 pending, T4 prior reporting preserved. No operational guard changes, new tests/probes/APT or Git delivery. Assessment summary retained privately as `evidence/cdots-phase5a-bqe3_j1j/isolation-design-assessment.txt`.

## D1 offline integration: task-registry incident

S17 grants offline integration and one conditional inert run only after combined offline tests and independent source audit pass. Parent observed selected-integrity exclusion RED1→21-test GREEN0 and fixed-proc-collector RED1→3-test GREEN0, with synthetic private worktree fixtures only. Exact streams/exits: `evidence/parent-d1-offline/`.

Two writer launches returned `mv5va7in-8-qy3j` and `mv5va7rv-9-blzz`; an explicit user-requested state check returned **no task** for both, and one catalog listed only older tasks. Current session/worktree identity is unchanged. Actual execution/termination state is unknown; no competing relaunch or source-write continuation is justified.

Retained kernel tests have RED exit1 (`isolation_checks` module missing). Mount source changed and final capture filenames exist, but final exit/stderr files read empty—**not GREEN evidence or completion**. Preserve partial artifacts; do not infer no execution, successful completion or stopped workers from registry absence. Cause is not established.

T1 is BLOCKED on original-task execution-state/report recovery or confirmed termination. Runner wiring, combined current suite, independent source audit, live isolation and native/E2E gates remain unmet. No new runtime/proc/package/overlay reads, containers, APT or delivery occurred in parent offline work. The unused conditional inert grant remains gated off. Risk: item2, security/evidence guards; incomplete integration is not accepted.
