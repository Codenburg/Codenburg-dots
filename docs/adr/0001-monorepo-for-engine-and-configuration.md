# ADR 0001: Monorepo for engine and configuration

- **Status:** Accepted (decision not implemented)

## Context

The project intends to develop a Linux environment manager alongside its software catalog, profiles, personal dotfiles, and documentation. Their ownership and evolution need a clear repository boundary.

## Decision

Keep the application engine and its configuration/catalog, profiles, personal dotfiles, and project documentation in one monorepo.

## Alternatives

Conceptual alternatives include separate repositories for the engine and configuration, or a single engine repository that excludes personal dotfiles. These are contrasts, not claims about historically evaluated proposals.

## Rationale

The intended product brings these related inputs together for one user-controlled environment-management workflow. One repository keeps their intended ownership and context together.

## Consequences

The repository is intended to contain distinct application, catalog/profile, personal-dotfile, and documentation responsibilities. Exact Go package boundaries and configuration serialization remain unresolved. This decision establishes repository organization, not implemented capabilities.
