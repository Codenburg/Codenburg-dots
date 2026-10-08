# Intended architecture

Codenburg Dots is planned as a provider-neutral Go monorepo application and catalog for managing selected Linux software and personal dotfiles. The repository currently implements a read-only CLI for system summaries, APT package inspection, and desired-present software plans; see [README](../README.md) for current commands. Beyond that implemented subset, this document defines intended architecture.

## Intended management lifecycle

The planned engine processes a request in this exact order:

1. **Detect** relevant platform and current context.
2. **Select profile** manually.
3. **Select software and dotfiles** with the user.
4. **Resolve providers, dependencies, and versions.**
5. **Inspect current state.**
6. **Plan** all proposed changes.
7. **Confirm** the plan explicitly.
8. **Back up** affected existing files.
9. **Apply** the confirmed plan.
10. **Verify** resulting state.

Detection, inspection, provider/dependency/version resolution, and planning are read-only. A repository configuration change is not a hidden inspection side effect: it must be shown as a planned mutation and confirmed before application. The eventual main UX is intended to be a TUI; its framework and any CLI libraries remain unresolved.

## Boundaries and invariants

- The core model is provider-neutral. Software is selected by logical catalog entry; provider-specific installation variants are explicit options, not hidden precedence rules.
- The engine should express desired state declaratively and idempotently. When requested state is already satisfied, it should avoid unnecessary work.
- The implemented software resolver expands declared logical software dependencies before dependents, preserves declaration/request order, deduplicates IDs, and rejects cycles or invalid references. The built-in catalog contains git, bash, and neovim with explicit APT variants and no declared dependencies. General resource dependencies, such as a Neovim configuration requiring Neovim, remain future work.
- The read-only planner reuses APT inspection and labels desired-present entries as none, install, unavailable, or error. Installed software needs no action even with a newer candidate; there is no upgrade action. Incomplete inspection plans retain ordered entries and contextual errors. Resolution/environment failures stop before package queries; there is no provider fallback.
- Every mutation belongs in the displayed plan. Apply follows explicit confirmation, and verification follows application.
- Package removal, user-file deletion, destructive cleanup, and silent overwrite are outside v0.x scope. See [Safety](SAFETY.md), the policy owner.
- A future read-only `cdots discover` may inspect the distribution and architecture, available providers, known installed software and versions, known dotfiles, and unmanaged items where feasible. Discovery must not import, generate, or apply configuration.

## v0.1 platform boundary

The initial planned platform family is Debian, Ubuntu, and Linux Mint, with APT (`apt-get`) and Flatpak/Flathub. Flatpak is a first-class provider. If Flathub setup is needed, configuring it is a separately visible, confirmed plan action—not an inspection effect. Adding an external APT repository, notably the official Brave repository, is likewise explicit and planned; repository removal is out of scope.

Arch Linux, `pacman`, `paru`, npm and GitHub Release providers, direct `.deb` packages, archives, fonts as an additional installation category, and other distributions are future possibilities, not v0.1 support. See [Configuration](CONFIGURATION.md) for provider and version concepts.

## Updates and distribution (future)

A future Debian-family update flow is intended to refresh package metadata, safely upgrade packages, update Flatpaks, update Codenburg Dots, then update dotfiles/repository content. Metadata refresh is itself a mutation, not inspection. The flow excludes destructive cleanup such as `apt autoremove`; this does not prescribe or freeze APT flags. Arch update support is future work.

Future distribution is expected to include `go install` and GitHub Release binaries for Linux `amd64` and `arm64`, eventually with checksums. There is no release automation or supported install procedure now.

## Open design questions

TUI/CLI libraries, serialization and schema, exact Go package boundaries, host-specific configuration, external configuration sources, plugins, arbitrary hooks, rollback UX beyond backups, and automatic hardware/profile detection are unresolved. Other distributions and providers are future scope, not established support.
