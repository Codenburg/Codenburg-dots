# Codenburg Dots

**Codenburg Dots** is an open-source personal Linux environment manager. The repository contains a read-only `cdots` CLI for Linux system summaries, APT package inspection, desired-present software plans, and whole-transaction APT preflight previews; installation, apply, profiles, dotfiles, and the TUI remain planned, not implemented.

## Try the CLI

Build with Go 1.22 or newer:

```sh
go build -o cdots ./cmd/cdots
./cdots
./cdots system
./cdots package <package-name>
./cdots plan bash git neovim
./cdots preflight bash git
./cdots preflight bash git neovim
```

The system summary reports distribution identity, `ID_LIKE`, architecture, supported status, and whether `dpkg-query`, `apt-cache`, and `dpkg` are available. Package inspection reports installed and APT candidate versions when available. It uses read-only package queries and Debian version comparison; it does not refresh package metadata or install, remove, upgrade, or change repositories.

`plan <software-id>...` selects logical IDs from the built-in catalog (`git`, `bash`, `neovim`), using their explicit APT packages. It validates the full selection before package queries, deduplicates IDs, and preserves request order (dependencies first if declared; the built-in entries declare none). Each entry shows software ID/name, provider, package, desired state, versions (`unknown` when absent), and a descriptive action:

- `none`: reliably installed, even if a newer candidate exists.
- `install`: not installed and an APT candidate is available; a label, not an operation.
- `unavailable` or `error`: no candidate for missing software, or inspection failed; contextual diagnostics are shown.

Incomplete plans retain useful entries on stdout and return exit code 2 with diagnostics on stderr. Invalid/unknown IDs, missing plan arguments, and environment failures also return 2; validation/environment failures perform no package queries. Successful commands return 0. No command applies a plan or upgrades installed software.

### APT transaction preflight

`preflight <software-id>...` reuses Phase 2 catalog resolution and APT inspection, then simulates one whole transaction for all resolved packages. Unlike `plan`'s desired-present labels, it reports the operations APT selects: installations, upgrades, removals, and configurations. The deterministic report distinguishes requested software/direct packages, declared catalog dependencies, and additional APT-selected packages, with known architectures/versions, risks, review requirements, uncertainty, and diagnostics. Unknown values remain explicit; missing versions, removal reasons, or negative protection flags are not invented.

| Outcome | Meaning | CLI exit |
| --- | --- | --- |
| `preview-complete` | Complete evidence, including a possible no-op | 0 |
| `review-required` / `high-risk` | Complete, valid inspectable preview; review or additional safeguards required, not automatic rejection | 0 |
| `unresolved` | Failed, incomplete, contradictory, or ambiguous evidence; retained operations and diagnostics remain visible | 2 |

Simulation success is independent of the safety assessment: an APT exit of 0 does not mean the transaction is safely actionable. Missing critical/Essential/Protected evidence is unresolved, not proof of an unprotected package. In independent read-only verification, `preflight bash git` was a no-op with CLI exit 0; `preflight bash git neovim` retained 13 installations and 13 configurations with a successful simulation but CLI exit 2 because protection fields were missing. These are host-specific snapshots, not guaranteed results.

Preflight requires `apt-get` in addition to the inspection tools, uses mandatory `--simulate` plus `APT::Get::Simulate=true`, and never changes the OS, downloads packages, refreshes metadata, installs, or applies anything. It bounds command time/output and narrowly parses APT's human output, which is not a stable machine API. The snapshot is non-atomic; non-root configuration visibility may differ. Removals are previews requiring review, not execution authority. Future apply would need revalidation and fresh operation-specific removal approval; neither authorization nor apply is implemented. See [ADR 0008](docs/adr/0008-apt-transaction-preflight-and-removal-authorization.md). Existing system, package, and plan commands keep their semantics.

The CLI explicitly supports Debian, Ubuntu, and Linux Mint IDs. `ID_LIKE` is diagnostic only and does not make other distributions supported. Package inspection and planning require those APT tools in `PATH`; no APT tools are needed for the system summary. Preflight currently supports `amd64` and `arm64`.

## Implemented and planned

| Area | Status |
| --- | --- |
| Go 1.22 standard-library CLI, system detection, APT package inspection | Implemented; commands above |
| Built-in software catalog, APT resolution, simple dependency expansion, desired-present planning and transaction preflight | Implemented; `plan <software-id>...` and `preflight <software-id>...`, read-only |
| Package installation or updates, Flatpak, profiles, external configuration loading, dotfiles, apply, TUI | Planned; not implemented |
| Debian, Ubuntu, Linux Mint and APT | Current read-only inspection/planning/preflight boundary |
| Other distributions/providers, including Arch | Future scope |

## Project foundation

The following documents describe intended design and boundaries; planned capabilities are not evidence of implementation:

- [Architecture](docs/ARCHITECTURE.md) — intended lifecycle, boundaries, and invariants.
- [Configuration](docs/CONFIGURATION.md) — conceptual catalog, profiles, providers, dependencies, and version information.
- [Safety](docs/SAFETY.md) — required v0.x mutation and file-protection policy.
- [Inventory](docs/INVENTORY.md) — user-reported notebook seed, not verified installed state.
- Accepted architecture decisions, not proof of implemented behavior:
  - [0001 — Monorepo for engine and configuration](docs/adr/0001-monorepo-for-engine-and-configuration.md)
  - [0002 — Go implementation](docs/adr/0002-go-implementation.md)
  - [0003 — Declarative and idempotent model](docs/adr/0003-declarative-idempotent-model.md)
  - [0004 — No destructive removal in v0.x](docs/adr/0004-no-destructive-removal-in-v0x.md) — historical; only its blanket future package-removal prohibition is superseded by ADR 0008.
  - [0005 — Debian-family first](docs/adr/0005-debian-family-first.md)
  - [0006 — User-controlled provider choice](docs/adr/0006-user-controlled-provider-choice.md)
  - [0007 — Selective, symlink-first dotfiles](docs/adr/0007-selective-symlink-first-dotfiles.md)
  - [0008 — APT transaction preflight and removal authorization](docs/adr/0008-apt-transaction-preflight-and-removal-authorization.md) — read-only Phase 3; future Phase 4 authorization/apply is not implemented.

There is no release, installer, or verified software inventory. The recorded personal inventory is a planning input and says nothing about this host's installed software.
