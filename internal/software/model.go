// Package software resolves logical selections without inspecting or changing the host.
package software

// Provider names can represent future catalog choices, but only APT is resolvable.
type Provider string

const APT Provider = "apt"

type Variant struct {
	Provider   Provider
	Identifier string
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
}

// Resolved retains the logical entry and the explicitly selected provider variant.
type Resolved struct {
	Software Software
	Variant  Variant
}
