# Safety and mutation policy

The following are required v0.x contracts, not implemented guarantees: no user-file deletion, no silent overwrite, and no destructive cleanup. Package removal is prohibited except within the narrowly scoped planned Phase 4 APT transaction contract below; there is no general-purpose package removal command. All system and file changes must be visible, confirmed, and safely handled.

## Required safeguards

- Keep detection, inspection, resolution, and planning read-only. Do not disguise a mutation as discovery or inspection.
- Put every mutation in a reviewable plan and require explicit user confirmation before applying it. This includes any `curl` use: it is permitted only in an explicit, understood bootstrap or repository workflow, never for read-only inspection.
- Never delete user files or perform destructive cleanup, including `apt autoremove`. Package removals must satisfy the planned Phase 4 APT transaction contract below; otherwise they are prohibited.
- Never silently overwrite an existing file. Report differing dotfiles and conflicts before mutation.
- Back up an existing file before replacing or modifying it. An illustrative future backup location is `~/.local/share/codenburg-dots/backups/`; this example is not a public API or established implementation path.
- Stop safely on failure and provide an actionable error rather than continuing with an unsafe partial operation.
- Store no secrets in the repository.

## Planned Phase 4 APT transaction contract

[ADR 0008](adr/0008-apt-transaction-preflight-and-removal-authorization.md) records the accepted direction now reflected in this normative contract. [ADR 0004](adr/0004-no-destructive-removal-in-v0x.md) remains unchanged as historical evidence; only its blanket package-removal prohibition has this narrow exception. These are planned requirements, not implemented guarantees: `cdots apply` is unimplemented, and read-only preflight never authorizes execution.

- **Review and authorization:** Preflight and authorization must precede execution. Every apply requires fresh explicit installation/transaction confirmation, including explicit review of indirect upgrades and all other APT-selected changes. Every ordinary package removal additionally requires separate, operation-specific approval identifying each package to be removed; generic confirmation or a reviewed preview is insufficient.
- **Fail-closed safety:** Essential or Protected package removals are absolutely forbidden, regardless of approval. This is not a blanket ban on upgrades: other package changes must meet the required safeguards, including additional safeguards for critical, Essential, or Protected package changes. Missing, contradictory, or otherwise unresolved safety metadata blocks execution; unknown protection status is never evidence of safety.
- **Revalidation:** Immediately before execution, revalidate the transaction against current state. Simulation is a non-atomic snapshot, not an execution guarantee. Material transaction changes invalidate authorization and require renewed review and transaction confirmation, plus renewed operation-specific removal approval where relevant.
- **Native safeguards:** Never bypass APT's native safeguards, use unsafe overrides, perform automatic autoremove, reuse unattended or persistent approval, or execute silent changes.
- **Failure reporting:** APT execution is not atomic. Stop safely on failure and accurately report package failures, completed operations, partial execution, and uncertain resulting state with actionable errors. Do not claim atomic rollback or automatic restoration.
- **Real installation tests:** Run them only in verified disposable environments, never on a user's working system.

## Dotfile and backup protections

The intended default for selected dotfiles is symlink-based management, with the repository as source of truth. If a destination already exists, inspect and compare it, report differences or conflicts, request confirmation, back it up, then apply the confirmed action. The detailed dotfile contract is in the accepted [symlink-first decision](adr/0007-selective-symlink-first-dotfiles.md).

Backups are a protection before replacement or modification; they do not imply a rollback UX, automatic restoration, or transactional guarantees. Detailed recovery and non-APT partial-application behavior beyond stopping safely with actionable errors remain to be designed; APT failure reporting must satisfy the contract above. See [Architecture](ARCHITECTURE.md) for lifecycle ordering and [Configuration](CONFIGURATION.md) for planned resources.
