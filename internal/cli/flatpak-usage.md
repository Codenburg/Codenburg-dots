# Read-only Flatpak CLI

## Commands

```text
cdots flatpak
cdots flatpak --scope=user
cdots flatpak --scope=installation:work
cdots flatpak --remote=flathub --arch=x86_64 --branch=stable org.mozilla.firefox
cdots plan --provider=apt firefox
cdots plan --provider=flatpak --scope=user --arch=x86_64 firefox
```

Flags must precede IDs. Both `--name=value` and `--name value` work.
Duplicate, unknown, empty, trailing, and incompatible flags fail before tool detection
or host inspection. Inventory accepts only scope; remote/arch/branch require an app ID.
There is no Flatpak apply, provider fallback, remote modification, metadata refresh,
installation, update, removal, or repair command.

`plan firefox` requires explicit provider selection. Firefox maps to APT
`firefox-esr` or Flatpak `org.mozilla.firefox`, remote `flathub`, branch `stable`.
Unflagged legacy APT-only requests such as `plan git bash` retain their output.
Alternatives in dependency selections also require an explicit provider. The selected
provider applies to the entire request, including dependencies. Conflicting catalog
metadata and requested filters are rejected; filters do not replace catalog constraints.
Only literal catalog defaults are used, with no host architecture conversion or inference.

## Observations and failures

Empty scope observes user and default-system installations. Named installations are
queried only when explicitly selected. Inventory shows scopes, remote names and disabled
state, and installed app ID/architecture/branch/origin/version. App inspection and plans
show requested filters, installed identities, cached candidate identities, state, and
all diagnostics. Repeated observations produce deterministic human-readable output.
Versions are opaque; blank versions render as `unknown`. Candidate data may be stale,
and neither cached refs nor installed versions prove candidate freshness, exact version,
or installation feasibility. No desired-present plan upgrades an installed application.

Multiple matching identities are ambiguous, including the same app in user and system
scopes. Narrow the scope/remote/architecture/branch explicitly. An `installed` inspection
state describes one observed match, not globally proven uniqueness when a list is
incomplete. Valid rows remain visible alongside diagnostics. Plans reject incomplete
installed-list evidence; optional remote failures remain visible even with action `none`.
Unavailable means no match in complete local observations, not global nonexistence.
Missing tools, failed sandbox execution, malformed output, and partial evidence fail
closed with diagnostics. Read-only failures use the existing CLI error exit code (2).

## Integration boundary and risks

`cli.RunReadOnly(ctx, args, stdout, stderr, detector, aptInspector, newFlatpak)`
accepts an injected lazy factory returning `cli.FlatpakInspector`. It validates the
selection before factory invocation and propagates cancellation to Flatpak inspection
and planning. `Run` keeps its original signature; without a factory, Flatpak paths
report unavailable. Main constructs `flatpak.NewExecRunner()` then `flatpak.New()`
only for validated Flatpak selections. No APT detection is needed on those paths.
`RunInteractive` and APT apply implementation are unchanged; main routes apply to that
entry point separately. APT and Flatpak planning use separate package APIs.

Production inspection requires Flatpak and Bubblewrap with read-only/offline isolation.
A sandbox can fail when Flatpak repository initialization requires writes; this is an
inspection failure, not permission to retry outside isolation. Live host compatibility
has not been validated by these deterministic CLI tests.

The software model now has literal identity fields and imports provider types; this is
an integration coupling to assess separately, not a generic provider registry. Cache/ref
evidence never guarantees installation. Foundation README, architecture, configuration,
safety, ADRs, and AGENTS status follow-up requires separate ownership approval; this
operational document does not replace them.
