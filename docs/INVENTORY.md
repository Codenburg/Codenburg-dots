# User-reported software and setup inventory

This is a notebook catalog seed supplied by the project owner. It is **not independently verified installed state**, a complete machine inventory, or a list of supported v0.1 packages. Exact reported labels are preserved; in particular, `herdr` is unresolved and is not interpreted here. Candidate classifications are investigation hints only, never verified installation recipes or identifiers.

## Reported software and setup items

| Reported label | Cautious planning classification |
| --- | --- |
| Brave Browser | Initial Debian-provider candidate; official APT repository may be required, subject to verification. |
| LocalSend | Candidate provider/category unverified. |
| ZapZap / Whatsie | Reported alternatives/combined label; provider and exact choice unverified. |
| Flathub | Flatpak provider/source setup; prerequisite is user-supported, exact setup procedure unverified. |
| Flatseal | Flatpak candidate; identifier and availability unverified. |
| Alacritty | Software candidate and separate configuration area reported; provider unverified. |
| Fish | Software candidate and configuration area reported; provider unverified. |
| JetBrains Nerd Fonts | Font/system-configuration candidate; mechanism unverified and fonts are not a v0.1 installation category. |
| Pi / Gentle Shell | Reported software/tooling label; provider and exact identity unverified. |
| OpenCode | Software/provider candidate unverified. |
| Node.js LTS | Software candidate; version/provider details unverified. |
| Python 3.13 | Software candidate; version-specific availability/provider unverified. |
| Gentle AI | Software candidate and PATH configuration area reported; provider unverified. |
| KeePassXC | Initial Debian-provider candidate; exact identifier and availability unverified. |
| Git | Initial Debian-provider candidate; exact identifier and availability unverified. |
| Go | Initial Debian-provider candidate; version and provider unverified. |
| FNM | Software candidate and Fish integration area reported; provider unverified. |
| Obsidian | Software candidate and setup area reported; provider unverified. |
| Balena Etcher | Software candidate; provider and system requirements unverified. |
| Telegram | Software candidate; provider unverified. |
| flashrom | Software/system candidate; provider and hardware requirements unverified. |
| GitHub CLI | Initial Debian-provider candidate; exact identifier and availability unverified. |
| Zed | Software candidate; external repository/provider requirement unverified. |
| `herdr` | Unresolved exact reported label; identity, category, and provider unknown. |

The table contains 24 reported rows, counting `ZapZap / Whatsie` as supplied. Initial Debian-provider candidates are not installation instructions; no package identifiers or version-specific availability are asserted. Brave's official APT repository and Flathub prerequisites are user-supported leads, not verified operational procedures. Other external-repository or provider requirements must not be inferred from these labels.

## Reported setup areas

These are seven user-reported areas, not verified configuration files, links, or implemented features:

1. Fish configuration.
2. Alacritty configuration.
3. Gentle AI PATH configuration.
4. FNM integration with Fish.
5. Obsidian setup.
6. Dark-mode setup.
7. Git/GitHub-backed notes.

Configuration-related labels are candidates for selective dotfile management only after the source, destination, and user intent are established. The conceptual example `dotfiles/fish/config.fish` to `~/.config/fish/config.fish` is not a real repository file or link.

## Scope and evidence limits

The v0.1 target remains selected software/packages and selected dotfiles on Debian, Ubuntu, and Linux Mint through APT and Flatpak/Flathub. The inventory does not assert that every listed item belongs in v0.1, is available through those providers, or is installed. Fonts as an added installation category, external configuration sources, and additional providers remain future or unresolved scope. No host discovery or inventory import has been performed.
