# Safety and mutation policy

The following are required v0.x contracts, not implemented guarantees: no package removal, no user-file deletion, no silent overwrite, and no destructive cleanup. All system and file changes must be visible, confirmed, and safely handled.

## Required safeguards

- Keep detection, inspection, resolution, and planning read-only. Do not disguise a mutation as discovery or inspection.
- Put every mutation in a reviewable plan and require explicit user confirmation before applying it. This includes any `curl` use: it is permitted only in an explicit, understood bootstrap or repository workflow, never for read-only inspection.
- Never remove packages, delete user files, or perform destructive cleanup, including `apt autoremove`.
- Never silently overwrite an existing file. Report differing dotfiles and conflicts before mutation.
- Back up an existing file before replacing or modifying it. An illustrative future backup location is `~/.local/share/codenburg-dots/backups/`; this example is not a public API or established implementation path.
- Stop safely on failure and provide an actionable error rather than continuing with an unsafe partial operation.
- Store no secrets in the repository.

The intended default for selected dotfiles is symlink-based management, with the repository as source of truth. If a destination already exists, inspect and compare it, report differences or conflicts, request confirmation, back it up, then apply the confirmed action. The detailed dotfile contract is in the accepted [symlink-first decision](adr/0007-selective-symlink-first-dotfiles.md).

Backups are a protection before replacement or modification; they do not imply a rollback UX, automatic restoration, or transactional guarantees. Failure and partial-application behavior beyond stopping safely with actionable errors remains to be designed. See [Architecture](ARCHITECTURE.md) for lifecycle ordering and [Configuration](CONFIGURATION.md) for planned resources.
