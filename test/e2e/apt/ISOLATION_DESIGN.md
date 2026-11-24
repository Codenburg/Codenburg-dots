# Phase 5A isolation evidence — conditionally accepted design D1

**Design accepted only. No isolation PASS, probe permission or APT authorization.** Independent assessor `mv5u41uh-7-80cp` accepted G1–G10 for conditional bounded inert validation, with no design blockers. T5 / S16 in `odd/tasks/phase5a-apt-e2e.md` owns this unit. Existing operational guards remain unchanged; offline integration, tests/source audit and a fresh execution grant are still required.

## Decision proposed

Require an image-bound, Phase5A-owned, live disposable container with verified effective access boundaries. Do not require resolving internal overlay link aliases or independently proving the ownership of every physical lower layer as a separate security gate.

This is a change to the proposed evidence model, not a claim that GraphDriver paths prove ownership. The previous provenance assessment remains correct for the previous model. The recorded 30-second metadata attempt proves neither current isolation nor this design's feasibility.

### Guarantees versus implementation assumptions

| Necessary guarantee | Evidence required | Not a substitute |
| --- | --- | --- |
| Selected pinned image | Supported image inspect ID/digest/config, then matching owned-container Image/ImageDigest | Tag, RootFS path or layer count |
| Only owned disposable resources | Exact full CID, unique name and fresh run-owner label, matched before every operation | `--latest`, partial ID or global cleanup |
| Current effective isolation | Bracketed running inspect, init PID/start-time binding and kernel namespaces/maps/mounts/security | Created inspect, old probe or runtime-wide mappings |
| No unauthorized host mounts or inherited access | Declared mounts/devices/FD policy plus complete effective mount tree and private namespace checks | `Mounts=[]` alone or lexical path prefixes |
| Host package protection | No host package paths/sockets/home shares; excluded host identities and privilege boundaries; selected before/after integrity evidence | Hash equality alone or unreadable lock hashes |
| Disposable writes and lifecycle | Engine-created image rootfs/private writable layer, no persistent volumes; retained evidence followed by owned stop/removal verification | Deleting graph storage or claiming cache layers were removed |

Overlay layer directory ownership, alias spelling, lowerdir entry counts and `../diff1` topology are backend details under the trust boundary below. Directly resolving them would not attest image file contents, exclude other mounts, prove namespace isolation, or defeat a compromised runtime. Their absence is not automatically a host-isolation failure. Unexpected effective mounts, incompatible rootfs configuration, or unexplained contradictions remain failures.

## Trust boundary and honest limits

The mechanism trusts the operator-controlled local `/usr/bin/podman`, its OCI runtime and storage implementation, the Linux kernel, and the pinned base image/probe tooling before package execution. It assumes no malicious same-UID/root host actor is concurrently replacing runtime state, configuration, cached image data or evidence. Local supported inspection is an attestation by that runtime, not an independent cryptographic measurement of unpacked layer bytes.

Threats the mechanism must detect or prevent: unintended host shares, rootful/privileged execution, caller/root identity mappings, reused/shared namespaces, capability/security-policy escapes, stale or foreign resource targeting, non-disposable mounts, unsafe inherited descriptors, and unnoticed changes to the selected host package-state objects.

Excluded guarantees: proof against a compromised kernel/runtime/host account, full-host noninterference, continuous detection of changes later restored, authenticity of every cached file independent of Podman, all supply-chain threats, or absence of kernel vulnerabilities. Namespace separation is defense in depth, not a VM-strength boundary. If those stronger guarantees are required, do not pretend physical alias resolution provides them: choose a separate disposable VM/test machine and review its own trust boundary.

Mount validation must still detect ordinary unsafe runtime configuration even though the runtime implementation is trusted. No custom rootfs, arbitrary lowerdir inputs, extra devices, secrets, volumes, preserved descriptors, namespace joins or security-disable flags may be requested. Global configuration is not accepted merely because it belongs to the operator: its effective consequences must match the approved profile.

## Initial inert profile — not an APT profile

The first validation, if separately authorized, uses one new rootless container from the existing cached pin:

- image ID `94926f0fd968cd68ac486502185a5dd78687874a9ec7727f64fe2af696e28311`;
- digest `sha256:2c037a04925515fdd6ea85ea14a682d0e79931f5e9f5d07b6dbfc6ba12f9e858`;
- local `--remote=false`, private empty hooks directory, restricted existing host environment, `--pull=never`;
- `--userns=nomap`, user `0:0` inside the container, private PID/IPC/UTS/cgroup namespaces and a separate mount namespace; `--network=none`;
- `--cap-drop=ALL`, no additions, no new privileges, seccomp enabled with the trusted runtime's standard profile; no unconfined/profile-disabling overrides;
- no bind/volume/tmpfs overrides, devices, secrets, extra groups, preserved FDs, host env inheritance, custom rootfs or image-declared volumes; no pod join, automatic restart or auto-removal;
- clear/override image entrypoint and run an audited fixed shell wait loop with a TERM handler, rather than relying on PID1's default signal behavior; do not use `--init` to introduce another host binary mount;
- explicit minimal PATH, LANG/LC_ALL, HOME `/root` and a run-specific hostname; running inspect must match the chosen hostname, not accept arbitrary generated HOME/HOSTNAME;
- finite approved resource limits, capture bounds and owned cleanup. Unsupported flags or unavailable kernel/runtime features block, not trigger compatibility guesses.

The final exact argv/script and resource limits must be frozen in the implementation review before execution. This document specifies the security profile; it is not a ready-to-run command pasted into the current harness.

A zero-capability, no-network inert result is **not** proof for a network-enabled or capability-bearing APT profile. Before real E2E, separately review the minimal APT capability/network requirements, rerun effective preflight for that exact profile and obtain execution authorization. Image/runtime/userns/capability/network/mount/entrypoint changes invalidate earlier acceptance.

## Evidence gates G1–G10

All evidence carries a run ID, capture sequence, monotonic duration, native exit, exact stdout/stderr, deadline, timeout/overflow/cleanup state, producer and object identity. Keep raw evidence private in an owned non-symlink 0700 worktree directory. Derived assertions point to the raw records. A missing required field is UNKNOWN/BLOCKED, never a default PASS.

### G1 — Supported runtime and image declarations

Before creation, retain supported local `info` and exact pinned `image inspect`. Require rootless Linux/amd64 Podman 4.9.3, the approved runtime/backend profile and expected image ID/digest. Reject unsafe image config (including declared volumes/health hooks/entrypoint effects not explicitly overridden). Do not pull or repair the cache. Record RootFS/GraphDriver fields as diagnostics, not filesystem ownership evidence. A different supported version still requires profile review, not silent admission.

### G2 — Ownership and lifecycle identity

Create exactly one container with a fresh run token label and unique name, capture a strict 64-hex CID, then inspect that CID. Require exact ID/name/label/image/digest and the approved created profile. Before start, exec, inspection-based decisions, stop or removal, re-check those identities. Never enumerate or remove unrelated containers. A disappeared resource cannot be reconstructed from a stale snapshot.

### G3 — Live process binding and freshness

After start, capture running inspect before any cleanup. Require Running=true, no pause/restart/dead/error, positive State.Pid, unchanged Created/StartedAt, zero RestartCount and the expected command/environment/profile.

Open a bounded ordinary `/proc/<owned-init-PID>` reference and capture `stat` start-time, `status`, `uid_map`, `gid_map`, `mountinfo` and namespace links. A before/after proc start-time check and a second owned-CID running inspect bracket the capture; any exit, reuse, replacement or status drift fails closed. Match host `NSpid` to guest PID1, and guest `/proc/1` start-time/namespace evidence to that init process. Avoid inferring container identity from PID alone.

Host proc observations are necessary corroboration, not protected overlay reads. No `unshare`, sudo, permission change or ptrace attachment is allowed. If ordinary permission checks prevent a required proc observation, this design cannot validate on that host under current constraints. Guest text alone is not a fallback for missing independent kernel evidence.

A supported `podman top <full-CID> hpid pid huser hgroup capeff capprm capinh capbnd seccomp` record may corroborate process data. Only documented descriptors may be used: unknown descriptors can launch a `ps` fallback. Top output cannot replace missing mount/namespace/start-time evidence.

### G4 — Effective namespaces and identity mappings

Capture the controller's own namespace links/maps and actual unprivileged UID/GID/groups, not a hard-coded host identity. Require seven distinct container-versus-controller namespaces: user, mount, PID, network, IPC, UTS and cgroup. Require init and fixed exec collector to inhabit the same expected container namespaces. Reject host/ns/pod reuse even if inspect claims private mode.

Compose inspect child-to-parent UID/GID maps with runtime-declared parent-to-host maps, then require agreement with host-read init maps and guest-read maps in their documented reader coordinates. Reject malformed, overlapping, empty or unbounded ranges. The composed ranges must exclude host UID/GID 0, the invoking UID/GID and its supplementary groups; all process real/effective/saved/filesystem identities must be inside the approved subordinate ranges. `nomap` is a requested property that must be corroborated, not an authority inferred from its name. Record provenance for every coordinate conversion.

### G5 — Effective privileges, capabilities and confinement

Inspect must reject Privileged=true, devices, extra groups/capabilities, security-disable flags and unsafe namespace modes. Kernel init and exec-collector status must show NoNewPrivs=1 and Seccomp=2. Inert CapInh/CapPrm/CapEff/CapBnd/CapAmb must all be zero and agree with inspect/top; parse masks strictly, never ignore unknown bits. No ambient or inherited privilege is allowed.

Seccomp=2 proves a filter is active, not its exact policy. The standard filter's selection is trusted to the reviewed runtime; arbitrary custom profiles/unconfined overrides are rejected. Preserve active AppArmor/SELinux policy when the host supports it; absence is recorded as a platform limitation, not invented LSM proof. If the operator requires independently pinned filter/LSM provenance beyond this baseline, obtain and review that evidence separately before admitting the profile.

### G6 — Complete effective mount tree and rootfs

Use **host-read init mountinfo**, corroborated by guest PID1/exec mountinfo, rather than inspect Mounts alone. Compare semantic topology, filesystem/device IDs, flags and roots, translating only documented namespace-coordinate-dependent fields. Parse escapes strictly, retain all raw fields, reject duplicates/ambiguous covering mounts, unexpected propagation, unexplained discrepancies and all unclassified mounts.

Retain the existing strict module's complete pseudo-filesystem/device/proc-mask and read-only sysfs/cgroup checks. Do not fall back to its non-full root-only mode. Ordinary image directories such as `/var/lib/dpkg` must remain in the private image-root filesystem, not under a separate external share.

Require exactly one image-based root mount of the approved overlay profile and the normal engine-created disposable writable layer. Cross-check inspect/runtime declarations and actual root filesystem type/options. No custom-rootfs/volume origin or unexpected driver transition is accepted. Root superblock upper/work identifiers must be compatible with the selected running inspect, but this is runtime/mount correlation, not ownership proof.

Lowerdir strings/aliases and generated layers are retained unchanged as backend diagnostics. Do not demand equal lowerdir counts, resolve protected objects, or normalize `alias/../diff1` as though lexical normalization described kernel resolution. Nominal configured-store checks can flag obvious contradictions but can never establish physical safety by prefix alone. The positive rootfs-origin assurance is the trusted supported runtime's image/CID association plus its observed root mount and private write lifecycle—not independent attestation of unpacked bytes. If that trust assumption is unacceptable, select the VM alternative rather than describing it as proven physical provenance.

### G7 — No unauthorized host shares

Inspect must show no requested Binds/Mounts/VolumesFrom/devices/secrets. Effective mountinfo must reject host package database, host home, host runtime socket, arbitrary directory/file shares and unknown devices even when Mounts is empty. No controller runtime socket is passed into the container and no inherited privileged FD is requested/preserved.

The only implicit regular-file exceptions are the known runtime-generated `/etc/hosts`, `/etc/hostname`, `/etc/resolv.conf` and `/run/.containerenv`. Each must be exactly classified by filesystem type, single-file kernel root, destination, flags and current CID-specific runtime data association. Inspect's HostsPath/HostnamePath/ResolvConfPath corroborate the first three; `.containerenv` requires the reviewed CID-specific runtime path. Use the controller's own mountinfo to translate each approved runtime path into filesystem-root coordinates (covering mount root plus relative path), then require that exact root and device identity in the container mount record. Ambiguous covering mounts/translation block; never strip prefixes heuristically. Reject direct binding of the host's original files, arbitrary files with similar basenames, directory mounts, mismatched CID or overlapping covering mounts. These narrow disposable runtime-file exceptions do not authorize mounting host home or package directories. Their physical inode ownership is not inferred from their names; this correlation still assumes a trusted runtime/operator and is not independent tamper attestation.

Do not automatically whitelist a newly observed path or relax flags to match a fixture. An unknown profile requires a design decision, not successful-looking execution.

### G8 — Selected host package-state protection

Before container creation and after owned cleanup, use the existing selected-integrity strategy, not broad lock/cache hashing. Mandatory scope is `/var/lib/dpkg/status` and `/var/lib/dpkg/info`; optional stable paths must be explicitly enumerated/authorized before any read. No-follow, stable duplicate snapshots and metadata/object coverage must match the existing module's public contract. Missing/unreadable/unstable selected objects block; do not skip them.

Require equal selected before/after evidence. Report that exact scope only: symlink targets, other host files and changes restored between snapshots are not attested. A mismatch means BLOCKED/host-state uncertainty, not automatically a cdots defect or permission to restore files. Equality supports the boundary proof but does not replace it or prove global non-mutation. No host sentinel writes, package command or repair is part of inert validation.

### G9 — Evidence-first owned cleanup

Persist created/running inspect, live process/mount/namespace/security evidence and the pre-cleanup gate report before stop/removal. Then use only the exact owned CID to stop with a finite grace, re-inspect stopped state, remove and verify expected absence. Capture native outcomes separately; exit125/`[]` after removal is expected absence only when the error identifies that exact CID, not a universal successful inspect.

Record TERM versus runtime KILL escalation. PID1's TERM handling must be tested as lifecycle behavior, not described as apply-cancellation proof. Uncertain cleanup is BLOCKED. Never remove the shared image cache, prune, access/delete overlay objects or clean other resources. Disposable means the container/writable resource is removed, not that every runtime cache/process/storage object vanished.

### G10 — Failures and acceptance states

A structured report records each gate PASS, FAIL or UNKNOWN with raw-record references. No aggregate PASS unless all required observations exist and agree; no native review/authorization is manufactured from a report. Capture failures and safety failures are distinct.

| Observation | Result / action |
| --- | --- |
| API timeout/overflow/nonzero unexpected exit, malformed JSON/proc record | Capture incomplete; BLOCKED, persist exact outputs and owned cleanup only; no retry/ceiling increase |
| Foreign/stale CID, PID/start-time drift, absent required live state | BLOCKED; no package use or substitute stale evidence |
| Host/caller identity mapped, shared namespace, privilege/cap/security mismatch | FAIL safety; BLOCKED and owned cleanup |
| Unknown mount, unauthorized share, missing mask, contradictory rootfs/profile | FAIL or UNKNOWN; BLOCKED, no automatic whitelist |
| Backend alias spelling/count differs but all effective gates agree | Diagnostic under reviewed runtime trust; not independent physical proof and not a failure solely for that difference |
| Host selected snapshot differs or cannot be captured | BLOCKED/host-state uncertainty; no host repair |
| Cleanup cannot be independently confirmed | BLOCKED/lifecycle uncertainty |
| All inert gates pass under fresh authorization | Inert-profile validation only; not APT approval or READY_TO_INTEGRATE |

No ordinary observer promises an atomic snapshot. Bracketed observations, identity checks, fixed immutable privilege boundaries and immediate pre-use revalidation reduce races; explicit absence of competing host actors remains a trust assumption. Any material change invalidates earlier evidence.

## Offline verification plan

This first pass changes only the design, not executable validators. There is no meaningful RED for prose; the checks are structural readback and independent safety/design assessment. Do not manufacture passing tests that only assert a proposed document says PASS. Historical 52/14/20 results remain historical.

Before implementing/admitting the proposed profile, focused offline tests through the validator APIs must cover:

| Rule | Positive control | Required rejection |
| --- | --- | --- |
| Pin + ownership | Exact ID/digest/CID/name/token | Any changed value or missing digest |
| Live binding | Bracketed stable PID/start-time/namespace records | Stale CID, replacement PID, changed start-time or restart |
| Map composition | nomap subordinate ranges agree in all frames | Root/caller/supplementary group included, overlap or frame mismatch |
| Capabilities/security | All-zero inert masks + NNP1/seccomp2 | Any capability bit, ambiguous mask, NNP0/seccomp0 or inspect contradiction |
| Namespace/mount coverage | Complete reviewed private profile | Host namespace, hidden/unknown/covering host mount, missing mask |
| Implicit files | Exact current-CID single-file associations | Host originals, foreign CID, same-name arbitrary source or directory |
| Overlay implementation detail | Retained aliases recorded without physical attestation | Unsafe effective mount still rejected despite plausible store prefix |
| Host integrity/lifecycle | Stable selected snapshots and owned cleanup | Selected unreadable/changed objects, incomplete cleanup |
| Fail-closed reducer | All mandatory raw observations present | Missing/stale evidence cannot become PASS |

Existing test subprocess-deny guards must remain; all synthetic inputs stay in memory or explicitly owned worktree scratch, with no Podman/package/CLI spawn, host-state reads or protected storage access. Current combined offline suite and independent source audit will be required after implementation. Designing this matrix is not reporting those tests executed or accepted.

## Next bounded authorization and fallback

If independently accepted, request a **separate** authorization for offline harness integration/source review and exactly one inert validation, conditional on all current offline checks and independent implementation review passing. Freeze final argv/scripts, record schema, exact read targets and budgets before executing. Proposed budget: at most30s per Podman capture, at most4MiB combined streams, at most600s overall including finite owned stop/removal; one container, no retries/pulls/APT/builds. Cleanup capability must be included in that grant.

Proposed read targets: controller's own `/proc/self` namespace/maps/mountinfo/identity; `/proc/<init-PID-of-exact-owned-CID>/{stat,status,uid_map,gid_map,mountinfo,ns/*}`; guest `/proc/1` and collector `/proc/self` equivalents through fixed read-only exec; selected host dpkg status/info only. Those targets are named for the future grant, **not read under S16**. No protected overlay directory, runtime socket, host home traversal or privilege escalation is included.

If required live proc evidence is inaccessible, runtime/config cannot be trusted, or mounts/identities cannot be classified without forbidden access, stop after owned cleanup and recommend a disposable Debian12 VM on a separate/test host: verified base image, fresh disk snapshot, no host shared folders/clipboard/agents/socket/device passthrough, copy artifacts rather than mount them, isolated network and explicit per-apply approval. Never install a hypervisor, download/boot a VM, or mutate the working host under this design authorization. VM setup and its own safety/evidence plan require a separate human decision.

## References and existing evidence

- Normative project safety: `docs/SAFETY.md`; intended lifecycle: `docs/ARCHITECTURE.md`. Their stale implementation descriptions are not proof about Phase4 source.
- Observed/retained profile: `fixtures/podman-4.9.3-running.json`, `fixtures/podman-4.9.3-mountinfo.txt`; old probe and metadata verdicts in `RESULTS.md`. No historical fixture is current isolation proof.
- Current change surfaces (not changed here): `runner.py`, `mount_checks.py`, `host_integrity.py` and focused tests. Strict full-mount and selected-integrity modules remain unintegrated.
- Version-specific supported interfaces: [Podman4.9.3 run](https://docs.podman.io/en/v4.9.3/markdown/podman-run.1.html), [container inspect](https://docs.podman.io/en/v4.9.3/markdown/podman-container-inspect.1.html), [image inspect](https://docs.podman.io/en/v4.9.3/markdown/podman-image-inspect.1.html), [top](https://docs.podman.io/en/v4.9.3/markdown/podman-top.1.html), [exec](https://docs.podman.io/en/v4.9.3/markdown/podman-exec.1.html).
- Kernel semantics: [user_namespaces(7)](https://man7.org/linux/man-pages/man7/user_namespaces.7.html). Latest Context7 results are supplemental, not4.9.3 feature/provenance proof.

## Independent assessment and implementation handoff

Assessor `mv5u41uh-7-80cp` accepted D1 at design level without blockers. Read-only assessment: no commands/tests/live evidence were executed. Its G1–G10 verdicts validate the design's requirements, not their implementation or availability. Parent structural readback remains supplied evidence.

Implementation must carry the offline matrix plus explicit rejection of PID reuse between brackets, missing host proc despite matching guest output, wrong filesystem/device correlation for implicit files, and selected-integrity reads outside the grant. Optional integrity paths must be excluded unless explicitly authorized. Preserve current guards until independently reviewed replacements exist. The current combined offline suite and independent source audit must pass before any inert execution.

Whether the same trust boundary and a changed network/capability profile are acceptable for eventual APT remains a separate decision. There is no authority to execute containers or APT from this design acceptance. Full Phase5A status remains BLOCKED. Retained assessment summary: `evidence/cdots-phase5a-bqe3_j1j/isolation-design-assessment.txt`.
