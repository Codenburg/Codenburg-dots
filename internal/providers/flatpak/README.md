# Read-only Flatpak provider

This package is independent of software resolution and planning. It implements
application inspection only; it cannot install, update, remove, refresh metadata,
or configure remotes.

## API for callers

- `NewExecRunner() (*ExecRunner, error)` detects Linux, Flatpak, and Bubblewrap
  executable paths without running either tool. Missing tooling is an error; no
  installation or unisolated fallback occurs. Construct `New(runner)` with that
  runner, or inject a deterministic `Runner` fixture.
- `Selection{ID, Scope, Remote, Arch, Branch}.Validate()` validates application IDs
  and optional filters without execution. Empty architecture/branch/remote are
  wildcards, not defaults. IDs must have at least three dot-separated components.
  Operands use a conservative ASCII name grammar; URLs, paths, and leading options
  are rejected. `Scope.Validate()` requires `user`, `system`, or `installation:NAME`.
- `Inventory(ctx, scope)` reads configured remotes (including disabled ones) and
  installed application refs. Empty scope means default system. Named installations
  are supported only when explicitly requested; there is no auto-discovery framework.
- `Inspect(ctx, selection)` queries user plus default system when selection scope
  is empty. It never silently chooses between matching installations, remotes,
  architectures, or branches. Explicit named scopes query only that installation.
- `Ref{Scope, Remote, ID, Arch, Branch, Version}` carries complete identity and an
  opaque optional version. `Ref.Validate()` validates the required identity.
  Do not identify instances by ID alone, compare versions using Debian ordering,
  or treat a missing version as absence.

Operational failures and malformed/partial rows are retained in `Diagnostics`,
with scope, operation, remote, and an error. Argument/initialization failures return
an ordinary error. Callers must consume both. Successful rows remain visible beside
failed rows; failed subprocess output is never used as positive evidence.

`Evidence.State` is:

- `installed`: one observed identity matching the installed selection, even if
  candidate metadata failed; accompanying uncertainty diagnostics are preserved.
  Partial installed evidence cannot prove a unique inventory. Planner/CLI callers
  reject incomplete installed-list evidence when establishing uniqueness.
- `available`: exactly one matching cached candidate and complete local observations.
- `ambiguous`: multiple matching installed identities or multiple candidate identities.
- `unknown`: insufficient/error evidence without a unique installed match.
- `unavailable`: no match in complete local observations (including no configured
  enabled remote), not a claim about global availability.

Candidates are **cache-backed observations**, not a promise of freshness,
installability, or an exact future installed version. Flatpak does not supply a
reliable freshness guarantee here; no stale version is invented or refreshed.
Disabled remotes provide no candidate evidence. Repeated identical identities are
diagnostic; conflicting versions are cleared while proven presence is retained.
Refs and remotes are sorted deterministically. Unknown remote option tokens fail
conservatively with diagnostics rather than assuming an enabled usable remote.

## Execution boundary

All production invocations use Bubblewrap, with a read-only root bind, mandatory
user/network/PID namespaces (`--unshare-all` plus explicit required flags), dropped
capabilities, disabled further user namespaces, a new session, parent-death handling,
fresh proc and minimal device mounts, and scratch tmpfs mounts at `/run` and `/tmp`.
No host device bind, writable inherited descriptors, stdin, or shell is used.
Bubblewrap's own environment is sanitized; `--clearenv` and controlled `--setenv`
values sanitize Flatpak's environment too. Locale is C, terminal/fancy output is
disabled, D-Bus endpoints are disabled, and only validated HOME/XDG_DATA_HOME are
preserved for per-user data. Paths hidden by scratch/proc/device mounts are rejected.
XDG configuration/runtime overrides and loader/Flatpak override variables are not
inherited. Nonstandard override-based installations are not supported implicitly.

Inspection has a ten-second deadline per subprocess (earlier caller cancellation
wins), four-MiB stdout and stderr limits per stream, and bounded pipe shutdown.
Timeouts, unsupported flags/namespaces, output overruns, and stderr fail closed;
there is no flag-removal retry. This is an allowlisted inspection boundary for
trusted installed tools, not a general sandbox for hostile arbitrary programs.

The only Flatpak command shapes, each with an explicit scope flag, are:

```text
list --app --columns=ref:full,origin:full,version:full
remotes --show-disabled --columns=name:full,options:full
remote-ls --cached --app --all --arch=* --columns=ref:full,version:full REMOTE
```

`REMOTE` comes from the scoped configured-remotes observation and validated names.
`--cached` is not the offline boundary: Flatpak can fall back to networking, which
mandatory network isolation prevents. Read-only root isolation also prevents
repository/configuration initialization or migration from touching host state.
An environment requiring such writes produces diagnostics rather than hidden changes.

Parsers require headerless tab-separated identity fields. Flatpak's table printer
emits a newline for an empty table and trims trailing empty cells; omitted final
remote-options/version cells are normalized to empty, never missing identity fields.
Parsers preserve blank versions and spaces in versions, accept `ID/arch/branch`
and explicit `app/ID/arch/branch`, and
reject runtime/other kind refs, missing identity fields, extra columns, controls,
and invalid UTF-8. `--app` is always present for application queries; runtime and
runtime-extension refs are not treated as selected applications.

Unit tests use injected rows and an ordinary test-executable helper for transport
limits/errors, never host Flatpak or Bubblewrap execution.
