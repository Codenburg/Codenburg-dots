# ADR 0005: Debian-family first

- **Status:** Accepted (decision not implemented)

## Context

The project needs a bounded initial platform and provider scope rather than implying support for every Linux distribution and package ecosystem.

## Decision

Target Debian, Ubuntu, and Linux Mint first, using APT (`apt-get`) and Flatpak/Flathub for the planned v0.1 software/package scope. Flatpak is a first-class provider. Configure missing Flathub only as an explicit planned and confirmed mutation.

## Alternatives

Conceptual alternatives include starting with multiple distribution families or limiting the initial scope to a single provider. These are contrasting approaches, not historical evaluation claims.

## Rationale

A focused family and two explicit providers bound initial platform scope while retaining both native-package and Flatpak choices. The decision does not claim that any individual inventory item is available from either provider.

## Consequences

Arch Linux, `pacman`, `paru`, npm and GitHub Release providers, direct `.deb` packages, archives, additional distributions, and fonts as an added installation category are future scope, not v0.1 support. External APT repository changes, including a possible official Brave repository, must be explicit plan entries; repository removal is out of scope. See [Architecture](../ARCHITECTURE.md).
