# ADR 0006: User-controlled provider choice

- **Status:** Accepted (decision not implemented)

## Context

A logical software item may be available through more than one installation provider, with different identifiers and system effects. An invisible provider preference can make changes surprising.

## Decision

Expose valid provider variants for a logical software entry and let the user choose. Do not impose hidden precedence such as native package before Flatpak. Future recommendations must be visible and overridable.

## Alternatives

Conceptual contrasts include silently preferring one provider or choosing a provider without showing alternatives. These are not claims about options historically evaluated.

## Rationale

Visible choice lets users review the source and consequences of installation. Provider neutrality at the core preserves that choice rather than hard-coding one ecosystem.

## Consequences

Plans should identify the selected provider and, where known, its package/application identifier and version information. For example, Brave could conceptually offer native APT and Flathub variants; no identifier, availability, or version is verified by that illustration. See [Configuration](../CONFIGURATION.md).
