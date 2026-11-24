# Apply execution-path trust diagnostics
Work only in the existing independent checkout; preserve the operator-validated absence fix.
No workers, VM/APT execution, network, commits, delivery, original-main edits or S24 changes.

## Specs
- S1: "The first fix for legitimate absent-package metadata has been manually validated on Debian 12. Do not undo it."
- S2: "Trace the exact source of the trust-path rejection and identify the rejected path or ancestor."
- S3: "Inspect the APT execution transport, including `/tmp/cdots-*`, ownership, permissions, and trust checks."
- S4: "Establish whether this is an incorrect rejection or a genuine security risk."
- S5: "Apply the smallest justified fix, preserving fail-closed behavior."
- S6: "Add regression and rejection tests; run offline Go tests and vet."
- S7: "Report the changes and exact build instructions for another manual Debian 12 VM retest."
- S8: "No changes to original main or S24. No real APT transactions, VM execution, network, new workers, commits, pushes, or merges."
- S9: "Do not bypass trust validation, broadly whitelist `/tmp`, or restart historical infrastructure recovery."

## Tasks
- C1 [blocked] — S2-S4: Await operator's full enriched VM error (requested/rejected path, UID/ownership, mode/sticky). Source rejection location is known; actual rejected VM node and genuine-risk versus false-positive classification are not. Route operator evidence; no commit.
- C2 [done] — S1-S3,S5-S6,S8-S9: Precise rejection diagnostics implemented with unchanged trust decisions; observed real-fixture RED then focused GREEN. Private FS seam hardwired to native functions in production. Actual VM classification not resolved. Route parent; no commit.
- C3 [done] — S1,S4,S6-S9: Full offline suite/vet passed, prior absence-fix hashes matched, formatting/diff checks clean. Build/retest instructions and explicit unresolved VM classification recorded. No native approval, worker verification or live execution claimed. Route parent; no commit.

## Log
### L1 — Original request, verbatim
```text
Continue the existing `cdots apply` bugfix in `/home/ace/Proyectos/cdots-apply-absence-fix`.

The first fix for legitimate absent-package metadata has been manually validated on Debian 12. Do not undo it.

The next real VM test completed all interactive approvals but failed with:

`untrusted execution path or ancestor`

`Execution attempted: true`, but Git remains uninstalled.

Reproduced using both `/home/tomas/cdots-fixed` and root-owned `/usr/local/bin/cdots-fixed`.

Tasks:
1. Trace the exact source of the trust-path rejection and identify the rejected path or ancestor.
2. Inspect the APT execution transport, including `/tmp/cdots-*`, ownership, permissions, and trust checks.
3. Establish whether this is an incorrect rejection or a genuine security risk.
4. Apply the smallest justified fix, preserving fail-closed behavior.
5. Add regression and rejection tests; run offline Go tests and vet.
6. Report the changes and exact build instructions for another manual Debian 12 VM retest.

No changes to original main or S24. No real APT transactions, VM execution, network, new workers, commits, pushes, or merges.

Do not bypass trust validation, broadly whitelist `/tmp`, or restart historical infrastructure recovery.
```

### L2 — Source facts and current evidence boundary
The exact message is emitted by trustedExecutionPath in internal/providers/apt/apply_execute_linux.go. It resolves symlinks, walks the resolved path and ancestors, rejects unavailable/non-root ownership and group/other writability, and has an existing root-owned sticky-/tmp exception. checkExecutionHost applies it to several native paths, APT configuration and /proc/self/exe; checkApplyMetadataHost also uses it. The generic error carries no requested/rejected path or mode. No VM filesystem metadata is available here: do not attribute the actual rejection to /tmp, the binary, or any other node, and do not relax policy speculatively. Current authorized change is precise diagnostics with unchanged decisions, not a claimed policy-bug correction.

The transport source previously inspected in this same baseline creates /tmp/cdots-* privately, a guard socket and private configuration, and cleans only its own directory. This alone does not show that transport caused this message; no new transport runtime or infrastructure discovery is authorized.

Preserve these current absence-fix SHA256 values:
- internal/providers/apt/apply_validation.go: 1d8b1f10aa5f4e6fe5e482e3480e2462866977a59dfab9c2f8ab87960532957e
- internal/providers/apt/apply_absence_test.go: 87c10c9ad4cf2606fdf13e0f7d2a3099361a161108ba2b4d437cd1d89293cd13

Discovery budget notice received; no configured explorer was launched because the human forbids new workers. Further inline work is limited to preparation and checking of the already-understood diagnostic change. Actual VM rejected-node proof remains blocked, not waived.

### L3 — Plan review and observed diagnostic RED
Reviewed task states after background completion: C1 remains blocked on actual VM node evidence; C2 remains in progress; C3 is ordered after final diagnostic tests. No task closed. The inert writable-file regression exited 1 because the existing function returned only the generic error, not requested/rejected path, UID, permission mode and sticky state. Stderr was empty. This is a diagnostic regression, not a reproduction or classification of the unknown VM filesystem condition. No trust acceptance rule or absence-fix code was changed.

### L4 — Focused diagnostic/policy GREEN and task transition
Focused TestTrustedExecutionPath* run exited 0 with empty stderr: actual writable-file diagnostic, 16 pure policy/ancestor fixtures and two lookup-failure cases passed. Root-owned trusted binary and sticky /tmp controls pass; non-root or writable binary/ancestors, non-root/non-sticky writable /tmp, sticky non-/tmp directories, unsafe private guard parent and missing ownership still reject. Resolver/stat failures remain fail-closed. Fixture callbacks verify complete expected ancestor traversal and no state mutation. This is local source-policy evidence, not actual VM state. C2 is done for diagnostic precision only; C3 is active for full-suite/report. C1 remains blocked. Prior absence-fix hashes still match.

### L5 — Final offline evidence and blocked VM classification
Go1.22.2 full offline go test ./... -count=1 -timeout=60s -json: exit0, eight packages,831passing test/subtest events, no failures. The new trust tests have21pass events (three test functions including16policy and2lookup subcases;19leaf cases total). go vet ./...: exit0 and empty stdout/stderr; suite stderr empty. Captures retained under .git/bugfix-checks/trust-{red,green,suite,vet}.*; intentionalRED1 thenGREEN0/suite0/vet0. gofmt-l and git diff--check clean.
Both L2 absence-fix SHA256 values still match. No original-main/S24 changes, workers, VM/APT execution, network, commits, pushes or merges. New work changes apply_execute_linux.go and adds apply_trust_linux_test.go plus this task document; previous absence-fix files remain present and unchanged.
The acceptance predicate is unchanged: symlinks resolve, each resolved node/ancestor must be root-owned and not group/other writable, with the existing exact root-owned sticky-/tmp exception. Native functions remain hardwired in the production wrapper. No broad /tmp whitelist, ownership bypass or runtime configuration override was added. The private filesystem seam exists only for deterministic fixture checks.
The actual rejected VM path is NOT established. Root-owned binary placement alone cannot identify every checked ancestor/native/configuration path. Source permits the same rejection before native process launch and during later rechecks; Execution attempted alone is not proof that a package transaction ran. A correctly owned sticky /tmp already passes the fixture control. It is not justified to call this a /tmp policy bug or to loosen the guard. C1 remains blocked on exact VM error/metadata, and no correctness-policy fix or successful VM apply is claimed.
Native assess was read-only and unassessable because untracked scope was undeclared; risk item2 explicitly recorded. This is an assessment preparation limitation, not native approval or proof of a missing verifier capability. No independent reviewer/verification worker was launched because the human forbids workers; no review authority recovery or historical infrastructure investigation occurred.

Build instructions for the operator (documented, not executed as a standalone build):
```sh
cd /home/ace/Proyectos/cdots-apply-absence-fix
(
  umask 022
  env GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off \
    GOTELEMETRY=off GOFLAGS= CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    /usr/bin/go build -mod=readonly -buildvcs=false \
    -o cdots-trust-diag ./cmd/cdots
)
```
Use the existing offline operator transfer/install procedure and a separate root-owned executable in the reviewed /usr/local/bin location, without replacing the validated binary or changing ancestor permissions merely to silence the guard. Repeat the existing manual approval flow only under the operator's authority. Return the full new line, including requested, rejected, uid or ownership, mode and sticky fields. If necessary, include read-only ownership/mode metadata for that exact rejected node and its ancestors; do not recursively chmod/chown or bypass validation. Classify and correct only once that evidence identifies the actual issue.

C2/C3 are complete only for the diagnostic patch and offline verification/report; S4 and the actual-node portion of S2 remain unfulfilled at C1. This is a bounded partial outcome, not completion of the original runtime failure fix.
