package software

import (
	"fmt"
	"regexp"
	"strings"
)

// Catalog is indexed by logical ID; lookup never infers a package from an ID.
type Catalog map[string]Software

var softwareID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func (c Catalog) Lookup(id string) (Software, error) {
	if !softwareID.MatchString(id) {
		return Software{}, fmt.Errorf("invalid software ID %q", id)
	}
	entry, ok := c[id]
	if !ok {
		return Software{}, fmt.Errorf("unknown software ID %q", id)
	}
	if entry.ID != id {
		return Software{}, fmt.Errorf("catalog ID mismatch: key %q contains software ID %q", id, entry.ID)
	}
	if strings.TrimSpace(entry.Name) == "" {
		return Software{}, fmt.Errorf("software %q has empty display name", id)
	}
	return entry, nil
}

// BuiltinCatalog returns a fresh catalog with no inferred dependencies.
func BuiltinCatalog() Catalog {
	return Catalog{
		"git": {
			ID: "git", Name: "Git",
			Variants: []Variant{{Provider: APT, Identifier: "git"}},
		},
		"bash": {
			ID: "bash", Name: "Bash",
			Variants: []Variant{{Provider: APT, Identifier: "bash"}},
		},
		// Mapping only: installation availability requires configured remote/cache evidence.
		"firefox": {
			ID: "firefox", Name: "Firefox",
			Variants: []Variant{
				{Provider: APT, Identifier: "firefox-esr"},
				{Provider: Flatpak, Identifier: "org.mozilla.firefox", Remote: "flathub", Branch: "stable"},
			},
		},
		"neovim": {
			ID: "neovim", Name: "Neovim",
			Variants: []Variant{{Provider: APT, Identifier: "neovim"}},
		},
	}
}
