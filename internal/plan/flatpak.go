package plan

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/Codenburg/Codenburg-dots/internal/providers/flatpak"
	"github.com/Codenburg/Codenburg-dots/internal/software"
)

// FlatpakInspector supplies read-only, local evidence, not installation guarantees.
// The production provider enforces configured remote/cache evidence for candidates.
// Injected implementations must obey that same contract.
type FlatpakInspector interface {
	Inspect(context.Context, flatpak.Selection) (flatpak.Evidence, error)
}

const Ambiguous Action = "ambiguous"

var ErrAmbiguous = errors.New("ambiguous Flatpak identity")

// FlatpakEntry keeps exact ref versions attached to their own identities. Versions
// are optional and opaque: desired-present planning never orders them or upgrades.
type FlatpakEntry struct {
	SoftwareID, DisplayName string
	Provider                software.Provider
	Selection               flatpak.Selection
	Desired                 Presence
	Current                 flatpak.Evidence
	Action                  Action
	Err                     error
}

type FlatpakPlan struct{ Entries []FlatpakEntry }

// BuildFlatpak resolves everything before inspection, retaining dependency/request
// order. This deliberately separate API cannot contaminate APT apply/preflight.
// The call-local cache includes scope, remote, ID, architecture, and branch.
// Unavailable describes complete local observations, not global nonexistence.
// Diagnostics remain visible even when presence satisfies the request; incomplete
// installed-list evidence cannot establish a unique installed identity.
func BuildFlatpak(ctx context.Context, catalog software.Catalog, desired software.Desired, inspector FlatpakInspector) (FlatpakPlan, error) {
	resolved, err := software.Resolve(catalog, desired)
	if err != nil {
		return FlatpakPlan{}, fmt.Errorf("resolve Flatpak plan: %w", err)
	}
	if desired.Provider != software.Flatpak {
		return FlatpakPlan{}, errors.New("Flatpak plan requires the Flatpak provider")
	}
	if inspector == nil {
		return FlatpakPlan{}, errors.New("Flatpak plan requires an inspector")
	}
	type observation struct {
		evidence flatpak.Evidence
		err      error
	}
	cache := map[flatpak.Selection]observation{}
	result := FlatpakPlan{Entries: make([]FlatpakEntry, 0, len(resolved))}
	failures := []error{}
	for _, item := range resolved {
		s := item.Variant.Selection()
		observed, ok := cache[s]
		if !ok {
			if err := ctx.Err(); err != nil {
				observed.err = err
			} else {
				observed.evidence, observed.err = inspector.Inspect(ctx, s)
			}
			observed.evidence = orderedEvidence(observed.evidence)
			cache[s] = observed
		}
		entry := FlatpakEntry{
			SoftwareID: item.Software.ID, DisplayName: item.Software.Name,
			Provider: software.Flatpak, Selection: s, Desired: Present, Current: observed.evidence,
		}
		entry.Action, err = compareFlatpak(s, observed.evidence, observed.err)
		if err != nil {
			entry.Err = fmt.Errorf("software %q provider %q selection %+v: %w", entry.SoftwareID, entry.Provider, s, err)
			failures = append(failures, entry.Err)
		}
		result.Entries = append(result.Entries, entry)
	}
	return result, errors.Join(failures...)
}

func compareFlatpak(s flatpak.Selection, e flatpak.Evidence, inspectionErr error) (Action, error) {
	causes := []error{}
	incompleteList := false
	for _, d := range e.Diagnostics {
		if d.Err == nil {
			causes = append(causes, errors.New("malformed Flatpak diagnostic without error"))
			incompleteList = true
			continue
		}
		causes = append(causes, fmt.Errorf("scope %q operation %q remote %q: %w", d.Scope, d.Operation, d.Remote, d.Err))
		diagnosticSelection := flatpak.Selection{ID: s.ID, Scope: d.Scope, Remote: d.Remote}
		invalidScope := d.Scope == "" || (s.Scope != "" && s.Scope != d.Scope)
		if s.Scope == "" && d.Scope != flatpak.User && d.Scope != flatpak.System {
			invalidScope = true
		}
		missingRemote := d.Operation == flatpak.RemoteLS && d.Remote == ""
		if err := diagnosticSelection.Validate(); err != nil {
			causes = append(causes, fmt.Errorf("malformed Flatpak diagnostic: %w", err))
			incompleteList = true
		}
		if invalidScope || missingRemote {
			causes = append(causes, errors.New("mismatched or incomplete Flatpak diagnostic identity"))
			incompleteList = true
		}
		if d.Operation == flatpak.List {
			incompleteList = true
		}
		if d.Operation != flatpak.List && d.Operation != flatpak.Remotes && d.Operation != flatpak.RemoteLS {
			incompleteList = true
		}
	}
	if inspectionErr != nil {
		return Error, errors.Join(append(causes, inspectionErr)...)
	}
	for _, refs := range [][]flatpak.Ref{e.Installed, e.Candidates} {
		seen := map[flatpak.Selection]bool{}
		for _, ref := range refs {
			if err := ref.Validate(); err != nil {
				return Error, errors.Join(append(causes, fmt.Errorf("malformed Flatpak ref: %w", err))...)
			}
			identity := flatpak.Selection{ID: ref.ID, Scope: ref.Scope, Remote: ref.Remote, Arch: ref.Arch, Branch: ref.Branch}
			if !selectionMatches(s, identity) || seen[identity] {
				return Error, errors.Join(append(causes, fmt.Errorf("mismatched or duplicate Flatpak ref: %+v", ref))...)
			}
			seen[identity] = true
		}
	}
	// Reconstruct the provider's state contract instead of trusting a state label.
	expected := flatpak.Unavailable
	switch {
	case len(e.Installed) > 1 || len(e.Candidates) > 1:
		expected = flatpak.Ambiguous
	case len(e.Installed) == 1:
		expected = flatpak.Installed
	case len(e.Diagnostics) > 0:
		expected = flatpak.Unknown
	case len(e.Candidates) == 1:
		expected = flatpak.Available
	}
	if e.State != expected {
		return Error, errors.Join(append(causes, fmt.Errorf("inconsistent Flatpak state %q, expected %q", e.State, expected))...)
	}
	switch e.State {
	case flatpak.Ambiguous:
		return Ambiguous, errors.Join(append(causes, ErrAmbiguous)...)
	case flatpak.Installed:
		if incompleteList {
			return Error, errors.Join(causes...)
		}
		return None, errors.Join(causes...)
	case flatpak.Available:
		return Install, nil
	case flatpak.Unavailable:
		return Unavailable, ErrUnavailable
	default:
		return Error, errors.Join(append(causes, errors.New("incomplete Flatpak evidence"))...)
	}
}

func selectionMatches(s, ref flatpak.Selection) bool {
	scopeMatches := s.Scope == ref.Scope
	if s.Scope == "" {
		scopeMatches = ref.Scope == flatpak.User || ref.Scope == flatpak.System
	}
	return s.ID == ref.ID && scopeMatches && (s.Remote == "" || s.Remote == ref.Remote) &&
		(s.Arch == "" || s.Arch == ref.Arch) && (s.Branch == "" || s.Branch == ref.Branch)
}

func orderedEvidence(e flatpak.Evidence) flatpak.Evidence {
	// Copy before sorting: injected evidence may share slices between calls.
	e.Installed = append([]flatpak.Ref{}, e.Installed...)
	e.Candidates = append([]flatpak.Ref{}, e.Candidates...)
	e.Diagnostics = append([]flatpak.Diagnostic{}, e.Diagnostics...)
	key := func(r flatpak.Ref) string {
		return string(r.Scope) + "/" + r.Remote + "/" + r.ID + "/" + r.Arch + "/" + r.Branch + "/" + r.Version
	}
	for _, refs := range [][]flatpak.Ref{e.Installed, e.Candidates} {
		sort.SliceStable(refs, func(i, j int) bool { return key(refs[i]) < key(refs[j]) })
	}
	sort.SliceStable(e.Diagnostics, func(i, j int) bool {
		return fmt.Sprint(e.Diagnostics[i]) < fmt.Sprint(e.Diagnostics[j])
	})
	return e
}
