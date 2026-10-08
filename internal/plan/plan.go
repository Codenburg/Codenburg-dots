// Package plan compares resolved software with injected observations. Actions are
// descriptive labels only; this package cannot install, upgrade, or apply changes.
package plan

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Codenburg/Codenburg-dots/internal/packages"
	"github.com/Codenburg/Codenburg-dots/internal/software"
)

// Inspector is satisfied by apt.Provider without an adapter. Provider selection
// belongs to the caller; Build does not discover tools or choose a fallback.
type Inspector interface {
	Inspect(context.Context, string) (packages.Info, error)
}

type Presence string

const Present Presence = "present"

type Action string

const (
	None        Action = "none"
	Install     Action = "install"
	Unavailable Action = "unavailable"
	Error       Action = "error"
)

// ErrUnavailable lets callers distinguish an unavailable package from an
// inspection failure; either makes Build return a non-nil aggregate error.
var ErrUnavailable = errors.New("package unavailable")

type Entry struct {
	SoftwareID  string
	DisplayName string
	Provider    software.Provider
	Package     string
	Desired     Presence
	Current     packages.Info
	Action      Action
	Err         error
}

type Plan struct {
	Entries []Entry
}

// Build resolves the entire selection before inspecting anything. Resolution
// errors return no entries. Observation errors and unavailable packages retain a
// dependency-first partial plan and return joined, contextual entry errors.
// Observations (including failures) are cached by provider and package only for
// this call, so distinct logical IDs retain entries but share current state.
func Build(ctx context.Context, catalog software.Catalog, desired software.Desired, inspector Inspector) (Plan, error) {
	resolved, err := software.Resolve(catalog, desired)
	if err != nil {
		return Plan{}, fmt.Errorf("resolve plan: %w", err)
	}
	if inspector == nil {
		return Plan{}, errors.New("plan requires an inspector")
	}
	type key struct {
		provider   software.Provider
		identifier string
	}
	type observation struct {
		info packages.Info
		err  error
	}
	cache := make(map[key]observation)
	result := Plan{Entries: make([]Entry, 0, len(resolved))}
	var failures []error
	for _, item := range resolved {
		k := key{provider: item.Variant.Provider, identifier: item.Variant.Identifier}
		observed, ok := cache[k]
		if !ok {
			if err := ctx.Err(); err != nil {
				observed.err = err
			} else {
				observed.info, observed.err = inspector.Inspect(ctx, k.identifier)
			}
			cache[k] = observed
		}
		entry := Entry{
			SoftwareID: item.Software.ID, DisplayName: item.Software.Name,
			Provider: k.provider, Package: k.identifier, Desired: Present,
			Current: observed.info,
		}
		entry.Action, err = compare(k.identifier, observed.info, observed.err)
		if err != nil {
			entry.Err = fmt.Errorf("software %q provider %q package %q: %w", entry.SoftwareID, entry.Provider, entry.Package, err)
			failures = append(failures, entry.Err)
		}
		result.Entries = append(result.Entries, entry)
	}
	return result, errors.Join(failures...)
}

func compare(identifier string, info packages.Info, inspectionErr error) (Action, error) {
	if inspectionErr != nil {
		return Error, inspectionErr
	}
	if info.Name != identifier {
		return Error, fmt.Errorf("inspection returned package %q, expected %q", info.Name, identifier)
	}
	for _, version := range []string{info.InstalledVersion, info.CandidateVersion} {
		if version == "(none)" || strings.ContainsAny(version, " \t\r\n") {
			return Error, fmt.Errorf("invalid observed version %q", version)
		}
	}
	installed := info.InstalledVersion != ""
	candidate := info.CandidateVersion != ""
	// Inspectors own version reliability and comparison. Only internally
	// consistent states are accepted; no lexical version comparison is needed.
	switch info.Status {
	case packages.Current, packages.UpdateAvailable:
		if installed && candidate {
			return None, nil
		}
	case packages.Unknown:
		// APT reports Unknown for a reliable installed version with no candidate.
		if installed && !candidate {
			return None, nil
		}
	case packages.Available:
		if !installed && candidate {
			return Install, nil
		}
	case packages.Unavailable:
		if !installed && !candidate {
			return Unavailable, ErrUnavailable
		}
	}
	return Error, fmt.Errorf("unknown or inconsistent inspection state: status %q, installed %q, candidate %q", info.Status, info.InstalledVersion, info.CandidateVersion)
}
