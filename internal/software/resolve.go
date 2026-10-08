package software

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	providerName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	// Match operands accepted by the APT inspector, including architecture qualifiers.
	aptIdentifier = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*(?::[a-z0-9][a-z0-9-]*)?$`)
)

// Resolve expands dependencies before dependents, preserving request and Requires
// order and returning each logical ID once. Errors discard any partial result.
// It performs no host/tooling checks and never falls back to another provider.
func Resolve(catalog Catalog, desired Desired) ([]Resolved, error) {
	if desired.Provider != APT {
		return nil, fmt.Errorf("unsupported provider %q: only APT resolution is supported", desired.Provider)
	}
	const (
		visiting = 1
		visited  = 2
	)
	states := make(map[string]int)
	var stack []string
	var result []Resolved
	var visit func(string) error
	visit = func(id string) error {
		switch states[id] {
		case visited:
			return nil
		case visiting:
			for i, ancestor := range stack {
				if ancestor == id {
					return fmt.Errorf("dependency cycle: %s -> %s", strings.Join(stack[i:], " -> "), id)
				}
			}
		}
		entry, err := catalog.Lookup(id)
		if err != nil {
			return err
		}
		variant, err := aptVariant(entry)
		if err != nil {
			return err
		}
		states[id] = visiting
		stack = append(stack, id)
		for _, dependency := range entry.Requires {
			if err := visit(dependency); err != nil {
				return fmt.Errorf("software %q requires %q: %w", id, dependency, err)
			}
		}
		stack = stack[:len(stack)-1]
		states[id] = visited
		result = append(result, Resolved{Software: entry, Variant: variant})
		return nil
	}
	for _, id := range desired.IDs {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func aptVariant(entry Software) (Variant, error) {
	seen := make(map[Provider]bool)
	var selected Variant
	for _, variant := range entry.Variants {
		if !providerName.MatchString(string(variant.Provider)) {
			return Variant{}, fmt.Errorf("software %q has invalid variant provider %q", entry.ID, variant.Provider)
		}
		if seen[variant.Provider] {
			return Variant{}, fmt.Errorf("software %q has duplicate variant provider %q", entry.ID, variant.Provider)
		}
		seen[variant.Provider] = true
		if variant.Provider == APT {
			selected = variant
		}
	}
	if !seen[APT] {
		return Variant{}, fmt.Errorf("software %q has no APT variant", entry.ID)
	}
	if !aptIdentifier.MatchString(selected.Identifier) {
		return Variant{}, fmt.Errorf("software %q has invalid APT package identifier %q", entry.ID, selected.Identifier)
	}
	return selected, nil
}
