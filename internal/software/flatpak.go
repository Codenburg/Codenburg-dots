package software

import (
	"fmt"

	"github.com/Codenburg/Codenburg-dots/internal/providers/flatpak"
)

// Selection preserves the full Flatpak identity without architecture conversion,
// branch defaults, or APT operand/version semantics. Only use for Flatpak variants.
func (v Variant) Selection() flatpak.Selection {
	return flatpak.Selection{ID: v.Identifier, Scope: v.Scope, Remote: v.Remote, Arch: v.Arch, Branch: v.Branch}
}

func validateFilters(d Desired) error {
	hasFilters := d.Scope != "" || d.Remote != "" || d.Arch != "" || d.Branch != ""
	if d.Provider != Flatpak {
		if hasFilters {
			return fmt.Errorf("Flatpak filters require the Flatpak provider")
		}
		return nil
	}
	// Validate optional operands even for an empty request, using a valid dummy ID.
	return (flatpak.Selection{ID: "org.example.App", Scope: d.Scope, Remote: d.Remote, Arch: d.Arch, Branch: d.Branch}).Validate()
}

func flatpakVariant(entry Software, d Desired) (Variant, error) {
	matches := []Variant{}
	seen := map[flatpak.Selection]bool{}
	for _, v := range entry.Variants {
		if !providerName.MatchString(string(v.Provider)) {
			return Variant{}, fmt.Errorf("software %q has invalid variant provider %q", entry.ID, v.Provider)
		}
		if v.Provider != Flatpak {
			continue
		}
		if err := v.Selection().Validate(); err != nil {
			return Variant{}, fmt.Errorf("software %q: %w", entry.ID, err)
		}
		if seen[v.Selection()] {
			return Variant{}, fmt.Errorf("software %q has duplicate Flatpak variant identity", entry.ID)
		}
		seen[v.Selection()] = true
		scope, scopeOK := constrain(string(v.Scope), string(d.Scope))
		remote, remoteOK := constrain(v.Remote, d.Remote)
		arch, archOK := constrain(v.Arch, d.Arch)
		branch, branchOK := constrain(v.Branch, d.Branch)
		if scopeOK && remoteOK && archOK && branchOK {
			v.Scope, v.Remote, v.Arch, v.Branch = flatpak.Scope(scope), remote, arch, branch
			matches = append(matches, v)
		}
	}
	switch len(matches) {
	case 0:
		return Variant{}, fmt.Errorf("software %q has no matching Flatpak variant", entry.ID)
	case 1:
		return matches[0], nil
	default:
		return Variant{}, fmt.Errorf("software %q has ambiguous Flatpak variants", entry.ID)
	}
}

func constrain(catalog, requested string) (string, bool) {
	if requested == "" {
		return catalog, true
	}
	if catalog != "" && catalog != requested {
		return "", false
	}
	return requested, true
}
