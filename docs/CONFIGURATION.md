# Configuration model

This document defines the planned, provider-neutral configuration concepts for Codenburg Dots. A minimal built-in software catalog and APT desired-present planner are implemented; no external configuration loader, serialization format, or final schema exists yet. The [architecture](ARCHITECTURE.md) owns lifecycle and system boundaries; [safety](SAFETY.md) owns mutation policy.

## Profiles and user choice

Profiles are manually selected recommendation sets: **desktop**, **laptop**, **netbook**, and **server**. A profile may suggest defaults only. There is no automatic hardware or host-specific profile detection in v0.1, and the user retains final control of every software and dotfile choice. Future recommendations should be visible and overridable, never silently applied.

## Software and providers

A logical software entry may expose multiple installation variants. For example, a conceptual Brave Browser entry could offer a native APT variant and a Flatpak variant from Flathub. This is an illustration, not a verified package/application identifier, recipe, or version claim. Valid variants are user choices; the system must not silently prefer native packages over Flatpak (or vice versa).

The intended v0.1 provider scope is APT (`apt-get`) and Flatpak/Flathub on Debian, Ubuntu, and Linux Mint. Flatpak is first class. Missing Flathub configuration may be added only as an explicit planned and confirmed mutation. The official Brave APT repository is a possible external repository requirement; such changes must also be explicit in the plan. Repository setup details and identifiers are not established here.

The implemented catalog contains logical IDs `git`, `bash`, and `neovim`, with display names and explicit same-named APT package variants. `cdots plan <software-id>...` selects APT explicitly with no fallback. The resolver supports declared logical software dependencies in dependency-first order with deduplication and cycle/reference diagnostics; these built-in entries declare no dependencies. This in-memory model does not establish a final configuration schema.

General resource dependencies remain planned. For example, a Neovim configuration could require Neovim; dotfile resources and their dependency handling are not implemented.

## Planned version and status information

Current read-only plans show the selected APT provider/package, desired state `present`, installed and candidate versions (`unknown` when absent), and action `none`, `install`, `unavailable`, or `error`. Reliably installed software is `none` even without a candidate or with a newer one. These labels do not install or upgrade anything; unavailable packages and inspection failures retain entries with contextual diagnostics and a nonzero CLI exit.

Beyond this implemented subset, where a future provider can reliably supply it, a plan should expose:

| Information | Intended meaning |
| --- | --- |
| Installed version | Version currently detected for the selected resource, if known. |
| Candidate/available version | Provider's available candidate, if reliably known. |
| Selected provider | User-selected installation source/variant. |
| Package/application identifier | Identifier used by that selected provider, once verified. |
| Resulting status | Expected state after the proposed operation. |

Unavailable or unknown version data must be labeled explicitly; the design does not promise that every provider exposes it. Already-installed software should be shown with its detected version when known, not blindly reinstalled.

## Scope boundaries

External configuration sources are not required for v0.1. Host-specific configuration, plugins, arbitrary hooks, and concrete schema/package boundaries remain unresolved. Future provider ideas include Arch (`pacman`, `paru`), npm, GitHub Releases, direct `.deb` packages, and archives; they are not current options or supported recipes. See the accepted decisions for [provider choice](adr/0006-user-controlled-provider-choice.md) and [Debian-family-first scope](adr/0005-debian-family-first.md).
