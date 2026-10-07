# Codenburg Dots

**Codenburg Dots** is an open-source personal Linux environment manager. This repository, `codenburg-dots`, is being established as a Go monorepo for the planned `cdots` application, software catalog, profiles, personal dotfiles, and project documentation. No application implementation exists yet; `cdots` commands described in this foundation are plans, not runnable instructions.

## Intended scope

The first planned release, v0.1, focuses on software/packages and selectively managed dotfiles for Debian, Ubuntu, and Linux Mint, using APT (`apt-get`) and Flatpak/Flathub. The intended experience is a TUI that shows a proposed, reviewable plan before changes. This document describes intended contracts and settled design decisions, not implemented capabilities.

## Foundation map

- [Architecture](docs/ARCHITECTURE.md) — intended lifecycle, boundaries, and invariants.
- [Configuration](docs/CONFIGURATION.md) — conceptual catalog, profiles, providers, dependencies, and version information.
- [Safety](docs/SAFETY.md) — required v0.x mutation and file-protection policy.
- [Inventory](docs/INVENTORY.md) — user-reported notebook seed, not verified installed state.
- Accepted architecture decisions, not proof of implementation:
  - [0001 — Monorepo for engine and configuration](docs/adr/0001-monorepo-for-engine-and-configuration.md)
  - [0002 — Go implementation](docs/adr/0002-go-implementation.md)
  - [0003 — Declarative and idempotent model](docs/adr/0003-declarative-idempotent-model.md)
  - [0004 — No destructive removal in v0.x](docs/adr/0004-no-destructive-removal-in-v0x.md)
  - [0005 — Debian-family first](docs/adr/0005-debian-family-first.md)
  - [0006 — User-controlled provider choice](docs/adr/0006-user-controlled-provider-choice.md)
  - [0007 — Selective, symlink-first dotfiles](docs/adr/0007-selective-symlink-first-dotfiles.md)

## Current repository status

This is a documentation foundation for a project bootstrap. There is no implemented binary, installer, runnable project command, release, or verified software inventory. The recorded personal inventory is a planning input and must not be read as evidence about this host's installed software.

## Planned distribution

Future distribution targets include `go install` and GitHub Release binaries for Linux `amd64` and `arm64`, with checksums eventually. No release automation or installation recipe exists yet.
