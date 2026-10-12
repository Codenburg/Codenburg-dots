// Package software resolves logical selections without inspecting or changing the host.
package software

import "github.com/Codenburg/Codenburg-dots/internal/providers/flatpak"

// Provider is always explicitly selected; resolution never chooses a fallback.
type Provider string

const (
	APT     Provider = "apt"
	Flatpak Provider = "flatpak"
)

type Variant struct {
	Provider   Provider
	Identifier string
	// Flatpak identity filters are literal, never inferred from the host.
	Scope                flatpak.Scope
	Remote, Arch, Branch string
}

type Software struct {
	ID       string
	Name     string
	Variants []Variant
	Requires []string
}

// Desired selects software to be present using one explicit provider for both
// requested entries and dependencies. It is not a serialized configuration schema.
type Desired struct {
	IDs      []string
	Provider Provider
	// Optional Flatpak filters constrain catalog variants and their observations.
	// Empty scope searches user and system; empty arch/branch stays unspecified.
	Scope                flatpak.Scope
	Remote, Arch, Branch string
}

// Resolved retains the logical entry and the explicitly selected provider variant.
type Resolved struct {
	Software Software
	Variant  Variant
}
