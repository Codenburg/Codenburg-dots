# ADR 0007: Selective, symlink-first dotfiles

- **Status:** Accepted (decision not implemented)

## Context

The intended manager includes personal dotfiles, but managing an entire home directory or silently replacing existing configuration would exceed user intent and risk data loss.

## Decision

Manage only explicitly selected dotfiles, defaulting to symlinks with the repository as source of truth. Before replacing or modifying an existing destination, inspect and compare it, report differences or conflicts, request confirmation, and back it up. Apply only the confirmed planned action.

## Alternatives

Conceptual contrasts include copying every repository file into the home directory, managing files without selection, or silently replacing destination files. These are not asserted to have been historically evaluated.

## Rationale

Selective links keep scope explicit and make the repository the intended source of truth, while inspection, confirmation, and backup protect pre-existing user files.

## Consequences

For example, a future conceptual mapping could be `dotfiles/fish/config.fish` to `~/.config/fish/config.fish`; neither path is asserted to exist. A future managed-block format using `# BEGIN CODENBURG-DOTS` and `# END CODENBURG-DOTS` is deferred for separate design. Backups do not imply rollback UX. See [Safety](../SAFETY.md).
