# ADR 0004: No destructive removal in v0.x

- **Status:** Accepted (decision not implemented)

## Context

An environment manager can encounter packages and files not owned by its configuration. Removing such state risks data loss or surprising changes to a user's system.

## Decision

In v0.x, never remove packages, delete user files, or perform destructive cleanup. In particular, do not run `apt autoremove`.

## Alternatives

Conceptual contrasts include reconciling desired state by automatically removing unselected resources or deleting conflicting user files. These are not claims about historical deliberation.

## Rationale

Preserving packages and user files bounds the risk of a first-generation manager. A user can review additions and changes without the tool claiming authority over unrelated state.

## Consequences

Removal and deletion are out of scope in v0.x, even where declarative state differs from current state. This does not prescribe or freeze APT flags. All other changes still require an explicit plan and confirmation, and existing files require backup before replacement or modification; see [Safety](../SAFETY.md).
