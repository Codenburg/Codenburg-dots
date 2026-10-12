package plan

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Codenburg/Codenburg-dots/internal/packages"
	"github.com/Codenburg/Codenburg-dots/internal/providers/flatpak"
	"github.com/Codenburg/Codenburg-dots/internal/software"
)

type flatpakInspectorFunc func(context.Context, flatpak.Selection) (flatpak.Evidence, error)

func (f flatpakInspectorFunc) Inspect(ctx context.Context, s flatpak.Selection) (flatpak.Evidence, error) {
	return f(ctx, s)
}

func firefoxRef() flatpak.Ref {
	return flatpak.Ref{ID: "org.mozilla.firefox", Scope: flatpak.User, Remote: "flathub", Arch: "x86_64", Branch: "stable"}
}

func TestBuildFlatpakEvidence(t *testing.T) {
	ref := firefoxRef()
	other := ref
	other.Scope = flatpak.System
	wrongID, wrongRemote, wrongArch, wrongBranch, wrongScope, malformed := ref, ref, ref, ref, ref, ref
	wrongID.ID, wrongRemote.Remote, wrongArch.Arch = "org.example.Other", "other", "aarch64"
	wrongBranch.Branch, wrongScope.Scope, malformed.Arch = "beta", "installation:other", ""
	failure := errors.New("observation failed")
	listFailure := []flatpak.Diagnostic{{Scope: flatpak.System, Operation: flatpak.List, Err: failure}}
	cacheFailure := []flatpak.Diagnostic{{Scope: flatpak.User, Operation: flatpak.RemoteLS, Remote: "flathub", Err: failure}}
	for _, tc := range []struct {
		name       string
		e          flatpak.Evidence
		inspectErr error
		action     Action
		cause      error
	}{
		{name: "presence unknown version", e: flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{ref}}, action: None},
		{name: "presence optional cache unavailable", e: flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{ref}, Diagnostics: cacheFailure}, action: None, cause: failure},
		{name: "presence list uncertainty", e: flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{ref}, Diagnostics: listFailure}, action: Error, cause: failure},
		{name: "different candidate identity not update", e: flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{ref}, Candidates: []flatpak.Ref{other}}, action: None},
		{name: "known ref unknown version", e: flatpak.Evidence{State: flatpak.Available, Candidates: []flatpak.Ref{ref}}, action: Install},
		{name: "unavailable local", e: flatpak.Evidence{State: flatpak.Unavailable}, action: Unavailable, cause: ErrUnavailable},
		{name: "uncertain availability", e: flatpak.Evidence{State: flatpak.Unknown, Candidates: []flatpak.Ref{ref}, Diagnostics: cacheFailure}, action: Error, cause: failure},
		{name: "uncertainty not global unavailability", e: flatpak.Evidence{State: flatpak.Unknown, Diagnostics: listFailure}, action: Error, cause: failure},
		{name: "ambiguous installed scopes", e: flatpak.Evidence{State: flatpak.Ambiguous, Installed: []flatpak.Ref{other, ref}}, action: Ambiguous, cause: ErrAmbiguous},
		{name: "ambiguous candidates", e: flatpak.Evidence{State: flatpak.Ambiguous, Candidates: []flatpak.Ref{other, ref}}, action: Ambiguous, cause: ErrAmbiguous},
		{name: "inspection error retains evidence", e: flatpak.Evidence{State: flatpak.Available, Candidates: []flatpak.Ref{ref}}, inspectErr: failure, action: Error, cause: failure},
		{name: "wrong ID", e: flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{wrongID}}, action: Error},
		{name: "wrong remote", e: flatpak.Evidence{State: flatpak.Available, Candidates: []flatpak.Ref{wrongRemote}}, action: Error},
		{name: "wrong arch", e: flatpak.Evidence{State: flatpak.Available, Candidates: []flatpak.Ref{wrongArch}}, action: Error},
		{name: "wrong branch", e: flatpak.Evidence{State: flatpak.Available, Candidates: []flatpak.Ref{wrongBranch}}, action: Error},
		{name: "wrong scope", e: flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{wrongScope}}, action: Error},
		{name: "malformed ref", e: flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{malformed}}, action: Error},
		{name: "duplicate refs", e: flatpak.Evidence{State: flatpak.Ambiguous, Installed: []flatpak.Ref{ref, ref}}, action: Error},
		{name: "incoherent available", e: flatpak.Evidence{State: flatpak.Available}, action: Error},
		{name: "incoherent installed", e: flatpak.Evidence{State: flatpak.Installed, Candidates: []flatpak.Ref{ref}}, action: Error},
		{name: "hidden ambiguity", e: flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{ref, other}}, action: Error},
		{name: "hidden diagnostic", e: flatpak.Evidence{State: flatpak.Unavailable, Diagnostics: listFailure}, action: Error, cause: failure},
		{name: "unknown state", e: flatpak.Evidence{State: "surprise"}, action: Error},
		{name: "nil diagnostic error", e: flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{ref}, Diagnostics: []flatpak.Diagnostic{{Operation: flatpak.List}}}, action: Error},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Most cases exercise an unspecified scope; constrain architecture explicitly.
			got, err := BuildFlatpak(context.Background(), software.BuiltinCatalog(),
				software.Desired{IDs: []string{"firefox"}, Provider: software.Flatpak, Arch: "x86_64"},
				flatpakInspectorFunc(func(context.Context, flatpak.Selection) (flatpak.Evidence, error) { return tc.e, tc.inspectErr }))
			if len(got.Entries) != 1 || got.Entries[0].Action != tc.action {
				t.Fatalf("plan %+v; error %v; want %s", got, err, tc.action)
			}
			wantErr := tc.action != None && tc.action != Install || tc.cause != nil
			if (err != nil) != wantErr || (got.Entries[0].Err != nil) != wantErr {
				t.Fatalf("plan %+v; error %v; want error %v", got, err, wantErr)
			}
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Fatalf("lost cause: %v", err)
			}
			if len(got.Entries[0].Current.Diagnostics) != len(tc.e.Diagnostics) {
				t.Fatal("lost diagnostics")
			}
		})
	}
}

func TestFlatpakCacheIdentityAndOrder(t *testing.T) {
	base := software.Variant{Provider: software.Flatpak, Identifier: "org.example.App", Scope: flatpak.User, Remote: "flathub", Arch: "x86_64", Branch: "stable"}
	variants := []software.Variant{base, base, base, base, base, base, base}
	variants[2].Scope = flatpak.System
	variants[3].Remote = "other"
	variants[4].Identifier = "org.example.Other"
	variants[5].Arch = "aarch64"
	variants[6].Branch = "beta"
	ids := []string{"root", "alias", "scope", "remote", "id", "arch", "branch"}
	catalog := software.Catalog{}
	for i, id := range ids {
		catalog[id] = software.Software{ID: id, Name: id, Variants: []software.Variant{variants[i]}}
	}
	root := catalog["root"]
	root.Requires = ids[1:]
	catalog["root"] = root
	wantOrder := append(append([]string{}, ids[1:]...), "root")
	var previous FlatpakPlan
	for run := 0; run < 3; run++ {
		calls := []flatpak.Selection{}
		got, err := BuildFlatpak(context.Background(), catalog, software.Desired{IDs: []string{"root", "alias"}, Provider: software.Flatpak},
			flatpakInspectorFunc(func(_ context.Context, s flatpak.Selection) (flatpak.Evidence, error) {
				calls = append(calls, s)
				return flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{{ID: s.ID, Scope: s.Scope, Remote: s.Remote, Arch: s.Arch, Branch: s.Branch}}}, nil
			}))
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != 6 {
			t.Fatalf("inspections %v; want 6 distinct full identities", calls)
		}
		order := []string{}
		for _, entry := range got.Entries {
			order = append(order, entry.SoftwareID)
		}
		if !reflect.DeepEqual(order, wantOrder) {
			t.Fatalf("order %v; want %v", order, wantOrder)
		}
		if run > 0 && !reflect.DeepEqual(previous, got) {
			t.Fatal("non-deterministic plan")
		}
		previous = got
	}
}

func TestFlatpakBoundariesBeforeQuery(t *testing.T) {
	for _, tc := range []struct {
		name    string
		desired software.Desired
	}{
		{name: "unknown later ID", desired: software.Desired{IDs: []string{"firefox", "missing"}, Provider: software.Flatpak}},
		{name: "native not Flatpak", desired: software.Desired{IDs: []string{"firefox"}, Provider: software.APT}},
		{name: "invalid filter", desired: software.Desired{IDs: []string{"firefox"}, Provider: software.Flatpak, Branch: "--help"}},
		{name: "no fallback", desired: software.Desired{IDs: []string{"git"}, Provider: software.Flatpak}},
		{name: "missing explicit provider", desired: software.Desired{IDs: []string{"firefox"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BuildFlatpak(context.Background(), software.BuiltinCatalog(), tc.desired,
				flatpakInspectorFunc(func(context.Context, flatpak.Selection) (flatpak.Evidence, error) {
					t.Fatal("query before valid complete resolution")
					return flatpak.Evidence{}, nil
				}))
			if err == nil || len(got.Entries) != 0 {
				t.Fatalf("plan %+v; error %v", got, err)
			}
		})
	}
	got, err := Build(context.Background(), software.BuiltinCatalog(), software.Desired{IDs: []string{"firefox"}, Provider: software.Flatpak},
		inspectorFunc(func(context.Context, string) (packages.Info, error) {
			t.Fatal("Flatpak ID reached APT inspector")
			return packages.Info{}, nil
		}))
	if err == nil || len(got.Entries) != 0 || !strings.Contains(err.Error(), "APT plan") {
		t.Fatalf("plan %+v; error %v", got, err)
	}
	if _, err := BuildFlatpak(context.Background(), software.BuiltinCatalog(), software.Desired{IDs: []string{"firefox"}, Provider: software.Flatpak}, nil); err == nil {
		t.Fatal("nil inspector accepted")
	}
}

func TestFlatpakCancellationAndCachedFailure(t *testing.T) {
	ref := firefoxRef()
	v := software.Variant{Provider: software.Flatpak, Identifier: ref.ID, Remote: ref.Remote, Branch: ref.Branch}
	catalog := software.Catalog{}
	for _, id := range []string{"first", "alias", "last"} {
		catalog[id] = software.Software{ID: id, Name: id, Variants: []software.Variant{v}}
	}
	last := catalog["last"]
	last.Variants = []software.Variant{v}
	last.Variants[0].Scope = flatpak.System
	catalog["last"] = last
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	got, err := BuildFlatpak(ctx, catalog, software.Desired{IDs: []string{"first", "alias", "last"}, Provider: software.Flatpak},
		flatpakInspectorFunc(func(received context.Context, s flatpak.Selection) (flatpak.Evidence, error) {
			calls++
			if received != ctx {
				t.Fatal("context not forwarded")
			}
			cancel()
			return flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{ref}}, received.Err()
		}))
	if calls != 1 || len(got.Entries) != 3 || !errors.Is(err, context.Canceled) {
		t.Fatalf("calls %d; plan %+v; error %v", calls, got, err)
	}
	for _, entry := range got.Entries {
		if entry.Action != Error || !errors.Is(entry.Err, context.Canceled) {
			t.Fatalf("entry %+v", entry)
		}
	}
	if !reflect.DeepEqual(got.Entries[0].Current, got.Entries[1].Current) {
		t.Fatal("cached failure lost evidence")
	}
}

func TestFlatpakVersionsStayOnExactRefs(t *testing.T) {
	installed, candidate := firefoxRef(), firefoxRef()
	installed.Version = "installed-opaque"
	candidate.Arch, candidate.Branch, candidate.Version = "aarch64", "beta", "candidate-opaque"
	catalog := software.Catalog{"app": {ID: "app", Name: "App", Variants: []software.Variant{
		{Provider: software.Flatpak, Identifier: installed.ID, Remote: installed.Remote},
	}}}
	got, err := BuildFlatpak(context.Background(), catalog, software.Desired{IDs: []string{"app"}, Provider: software.Flatpak},
		flatpakInspectorFunc(func(context.Context, flatpak.Selection) (flatpak.Evidence, error) {
			return flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{installed}, Candidates: []flatpak.Ref{candidate}}, nil
		}))
	if err != nil || len(got.Entries) != 1 || got.Entries[0].Action != None {
		t.Fatalf("plan %+v; error %v", got, err)
	}
	current := got.Entries[0].Current
	if current.Installed[0] != installed || current.Candidates[0] != candidate {
		t.Fatalf("lost exact version identities: %+v", current)
	}
	if got.Entries[0].Selection.Arch != "" || got.Entries[0].Selection.Branch != "" {
		t.Fatal("invented identity defaults")
	}
}

func TestFlatpakEvidenceOrder(t *testing.T) {
	user, system := firefoxRef(), firefoxRef()
	system.Scope = flatpak.System
	var previous string
	for run := 0; run < 4; run++ {
		refs := []flatpak.Ref{user, system}
		if run%2 == 0 {
			refs[0], refs[1] = refs[1], refs[0]
		}
		original := append([]flatpak.Ref{}, refs...)
		got, err := BuildFlatpak(context.Background(), software.BuiltinCatalog(), software.Desired{IDs: []string{"firefox"}, Provider: software.Flatpak},
			flatpakInspectorFunc(func(context.Context, flatpak.Selection) (flatpak.Evidence, error) {
				return flatpak.Evidence{State: flatpak.Ambiguous, Installed: refs}, nil
			}))
		if !errors.Is(err, ErrAmbiguous) {
			t.Fatalf("error %v", err)
		}
		if !reflect.DeepEqual(refs, original) {
			t.Fatal("mutated inspector-owned evidence")
		}
		if got.Entries[0].Current.Installed[0].Scope != flatpak.System {
			t.Fatal("unstable ref order")
		}
		if run > 0 && previous != err.Error() {
			t.Fatal("unstable error output")
		}
		previous = err.Error()
	}
}

type flatpakRunnerFunc func(context.Context, flatpak.Query) (string, error)

func (f flatpakRunnerFunc) Run(ctx context.Context, q flatpak.Query) (string, error) {
	return f(ctx, q)
}

func TestFlatpakConfiguredCacheEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, remotes, cache string
		failure              bool
		action               Action
	}{
		{name: "mapping is not availability", action: Unavailable},
		{name: "configured remote without cache ref", remotes: "flathub\t\n", action: Unavailable},
		{name: "configured exact cache ref", remotes: "flathub\t\n", cache: "app/org.mozilla.firefox/x86_64/stable\t\n", action: Install},
		{name: "wrong branch cache", remotes: "flathub\t\n", cache: "app/org.mozilla.firefox/x86_64/beta\t\n", action: Unavailable},
		{name: "wrong arch cache", remotes: "flathub\t\n", cache: "app/org.mozilla.firefox/aarch64/stable\t\n", action: Unavailable},
		{name: "malformed cache", remotes: "flathub\t\n", cache: "not-a-ref\t\n", action: Error},
		{name: "cache operation failure", remotes: "flathub\t\n", failure: true, action: Error},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cacheCalls := 0
			provider := flatpak.New(flatpakRunnerFunc(func(_ context.Context, q flatpak.Query) (string, error) {
				if q.Scope != "installation:work" {
					t.Fatalf("lost named scope: %+v", q)
				}
				switch q.Operation {
				case flatpak.List:
					return "", nil
				case flatpak.Remotes:
					return tc.remotes, nil
				case flatpak.RemoteLS:
					cacheCalls++
					if q.Remote != "flathub" {
						t.Fatalf("wrong remote %+v", q)
					}
					if tc.failure {
						return "", errors.New("cache unavailable")
					}
					return tc.cache, nil
				default:
					t.Fatalf("unexpected query %+v", q)
					return "", nil
				}
			}))
			got, err := BuildFlatpak(context.Background(), software.BuiltinCatalog(),
				software.Desired{IDs: []string{"firefox"}, Provider: software.Flatpak, Scope: "installation:work", Arch: "x86_64"}, provider)
			if len(got.Entries) != 1 || got.Entries[0].Action != tc.action || (err == nil) != (tc.action == Install) {
				t.Fatalf("plan %+v; error %v; want %s", got, err, tc.action)
			}
			if tc.remotes == "" && cacheCalls != 0 {
				t.Fatal("queried an unconfigured remote")
			}
		})
	}
}

func TestBuildFlatpakUnknownCandidateVersion(t *testing.T) {
	got, err := BuildFlatpak(context.Background(), software.BuiltinCatalog(),
		software.Desired{IDs: []string{"firefox"}, Provider: software.Flatpak},
		flatpakInspectorFunc(func(_ context.Context, s flatpak.Selection) (flatpak.Evidence, error) {
			if s.ID != "org.mozilla.firefox" || s.Remote != "flathub" || s.Branch != "stable" || s.Scope != "" || s.Arch != "" {
				t.Fatalf("invented or lost filters: %+v", s)
			}
			return flatpak.Evidence{State: flatpak.Available, Candidates: []flatpak.Ref{
				{ID: s.ID, Scope: flatpak.User, Remote: s.Remote, Arch: "x86_64", Branch: s.Branch},
			}}, nil
		}))
	if err != nil || len(got.Entries) != 1 || got.Entries[0].Action != Install {
		t.Fatalf("plan %+v; error %v", got, err)
	}
}
