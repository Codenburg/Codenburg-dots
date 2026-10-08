<!-- agents-md-manager:managed:start -->
## Project essentials
- This Go monorepo implements a read-only `cdots` CLI for system summaries, APT package inspection, and desired-present software plans using a minimal built-in catalog. `cdots apply` is unimplemented; other capabilities remain planned, and decisions alone are not proof of implementation. See [README.md](README.md).
- Planned v0.1 scope is Debian, Ubuntu, and Linux Mint with APT (apt-get) and Flatpak/Flathub. Arch (pacman/paru) is future scope.

## Context routing
- For project purpose, status, and the foundation map → [README.md](README.md).
- For intended lifecycle, architecture, boundaries, and platform scope → [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).
- For profiles, providers, configuration concepts, and version visibility → [docs/CONFIGURATION.md](docs/CONFIGURATION.md).
- For the user-reported notebook seed (not verified installed state) → [docs/INVENTORY.md](docs/INVENTORY.md).
- For accepted durable decisions (not proof of implemented capabilities), consult [ADR 0001 — monorepo](docs/adr/0001-monorepo-for-engine-and-configuration.md), [0002 — Go](docs/adr/0002-go-implementation.md), [0003 — declarative and idempotent model](docs/adr/0003-declarative-idempotent-model.md), [0004 — historical no-destructive-removal decision; blanket future package-removal ban superseded by ADR 0008](docs/adr/0004-no-destructive-removal-in-v0x.md), [0005 — Debian-family first](docs/adr/0005-debian-family-first.md), [0006 — user-controlled provider choice](docs/adr/0006-user-controlled-provider-choice.md), [0007 — selective, symlink-first dotfiles](docs/adr/0007-selective-symlink-first-dotfiles.md), and [0008 — APT transaction preflight and planned removal authorization](docs/adr/0008-apt-transaction-preflight-and-removal-authorization.md).

## Mandatory workflows
- For any Go coding, review, debugging, or setup task, first load [golang-how-to](.agents/skills/golang-how-to/SKILL.md) and use it to select and load the applicable installed Go skills; report missing skills rather than assuming they are available.
- When planning or implementing system or file mutations, follow [docs/SAFETY.md](docs/SAFETY.md) for permissions and safeguards and [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for intended lifecycle: inspection and planning are read-only; mutations require an explicit reviewable plan, and every apply requires fresh explicit confirmation. Only the narrowly scoped planned APT transaction contract permits ordinary removals, with separate operation-specific approval identifying each package; Essential/Protected removals are forbidden. Missing or contradictory safety evidence blocks execution; material transaction changes invalidate authorization. Follow the normative documents for native safeguards and immediate revalidation; no automatic autoremove or silent mutations.
- Never delete user files in v0.x. Compare existing dotfiles, report differences or conflicts, and back up before replacement or modification. Keep selected providers visible and expose version data only when reliably available.
- User-authorized tooling instruction (not product documentation): use CodeGraph structural/code graph tooling when useful; do not use Graphify.

## Source precedence
- For selected dotfiles, the repository is the intended source of truth ([docs/SAFETY.md](docs/SAFETY.md), [ADR 0007](docs/adr/0007-selective-symlink-first-dotfiles.md)). [docs/SAFETY.md](docs/SAFETY.md) owns mutation permissions and safeguards; [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) defines intended lifecycle. No general precedence among project sources is established.
<!-- agents-md-manager:managed:end -->