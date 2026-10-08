package plan

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Codenburg/Codenburg-dots/internal/packages"
	"github.com/Codenburg/Codenburg-dots/internal/software"
)

type inspectorFunc func(context.Context, string) (packages.Info, error)

func (f inspectorFunc) Inspect(ctx context.Context, name string) (packages.Info, error) {
	return f(ctx, name)
}

func fixture(id, pkg string, requires ...string) software.Software {
	return software.Software{
		ID: id, Name: "Display " + id, Requires: requires,
		Variants: []software.Variant{{Provider: software.APT, Identifier: pkg}},
	}
}

func TestActionTable(t *testing.T) {
	failure := errors.New("inspection failed")
	for _, tc := range []struct {
		name       string
		status     packages.Status
		installed  string
		candidate  string
		inspectErr error
		action     Action
	}{
		{"installed current", packages.Current, "2", "2", nil, None},
		{"installed newer candidate", packages.UpdateAvailable, "1", "2", nil, None},
		{"installed newer than candidate", packages.Current, "3", "2", nil, None},
		{"installed no candidate", packages.Unknown, "1", "", nil, None},
		{"missing available", packages.Available, "", "2", nil, Install},
		{"missing unavailable", packages.Unavailable, "", "", nil, Unavailable},
		{"inspection failure", packages.Available, "", "2", failure, Error},
		{"unknown empty", packages.Unknown, "", "", nil, Error},
		{"unknown candidate", packages.Unknown, "", "2", nil, Error},
		{"unknown installed and candidate", packages.Unknown, "1", "2", nil, Error},
		{"unrecognized status", packages.Status("other"), "", "2", nil, Error},
		{"empty status", "", "", "2", nil, Error},
		{"current without installed", packages.Current, "", "2", nil, Error},
		{"current without candidate", packages.Current, "1", "", nil, Error},
		{"update without installed", packages.UpdateAvailable, "", "2", nil, Error},
		{"update without candidate", packages.UpdateAvailable, "1", "", nil, Error},
		{"available without candidate", packages.Available, "", "", nil, Error},
		{"available with installed", packages.Available, "1", "2", nil, Error},
		{"unavailable with candidate", packages.Unavailable, "", "2", nil, Error},
		{"unavailable with installed", packages.Unavailable, "1", "", nil, Error},
		{"placeholder installed", packages.Unknown, "(none)", "", nil, Error},
		{"blank installed", packages.Unknown, " ", "", nil, Error},
		{"placeholder candidate", packages.Available, "", "(none)", nil, Error},
		{"blank candidate", packages.Available, "", " ", nil, Error},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := packages.Info{Name: "pkg", Status: tc.status, InstalledVersion: tc.installed, CandidateVersion: tc.candidate}
			got, err := Build(context.Background(), software.Catalog{"app": fixture("app", "pkg")},
				software.Desired{IDs: []string{"app"}, Provider: software.APT},
				inspectorFunc(func(context.Context, string) (packages.Info, error) { return info, tc.inspectErr }))
			if len(got.Entries) != 1 {
				t.Fatalf("entries = %+v", got.Entries)
			}
			entry := got.Entries[0]
			if entry.SoftwareID != "app" || entry.DisplayName != "Display app" || entry.Package != "pkg" || entry.Provider != software.APT || entry.Desired != Present || entry.Current != info || entry.Action != tc.action {
				t.Fatalf("entry = %+v; want action %s and original metadata", entry, tc.action)
			}
			wantErr := tc.action == Error || tc.action == Unavailable
			if (err != nil) != wantErr || (entry.Err != nil) != wantErr {
				t.Fatalf("build error = %v; entry error = %v; want error %v", err, entry.Err, wantErr)
			}
			if wantErr {
				for _, text := range []string{"app", "apt", "pkg"} {
					if !strings.Contains(entry.Err.Error(), text) || !strings.Contains(err.Error(), text) {
						t.Fatalf("missing %q context: entry %v; aggregate %v", text, entry.Err, err)
					}
				}
			}
			if tc.inspectErr != nil && !errors.Is(err, failure) {
				t.Fatalf("lost inspection cause: %v", err)
			}
			if tc.action == Unavailable && !errors.Is(err, ErrUnavailable) {
				t.Fatalf("lost unavailable cause: %v", err)
			}
		})
	}
}

func TestDependencyOrderAndDeterminism(t *testing.T) {
	catalog := software.Catalog{
		"app":   fixture("app", "app", "right", "left", "right"),
		"right": fixture("right", "right", "base"),
		"left":  fixture("left", "left", "base"),
		"base":  fixture("base", "base"),
	}
	var previous Plan
	for run := 0; run < 3; run++ {
		var calls []string
		got, err := Build(context.Background(), catalog, software.Desired{IDs: []string{"app", "base", "left"}, Provider: software.APT},
			inspectorFunc(func(_ context.Context, name string) (packages.Info, error) {
				calls = append(calls, name)
				return packages.Info{Name: name, Status: packages.Available, CandidateVersion: "1"}, nil
			}))
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, entry := range got.Entries {
			ids = append(ids, entry.SoftwareID)
		}
		want := []string{"base", "right", "left", "app"}
		if !reflect.DeepEqual(ids, want) || !reflect.DeepEqual(calls, want) {
			t.Fatalf("IDs %v; inspections %v; want %v", ids, calls, want)
		}
		if run > 0 && !reflect.DeepEqual(got, previous) {
			t.Fatalf("non-deterministic plan: %+v vs %+v", got, previous)
		}
		previous = got
	}
}

func TestResolveFailureDoesNotInspect(t *testing.T) {
	catalog := software.Catalog{"app": fixture("app", "pkg"), "broken": fixture("broken", "broken", "missing")}
	for _, desired := range []software.Desired{
		{IDs: []string{"app", "missing"}, Provider: software.APT},
		{IDs: []string{"app", "broken"}, Provider: software.APT},
		{IDs: []string{"app"}, Provider: "flatpak"},
	} {
		got, err := Build(context.Background(), catalog, desired, inspectorFunc(func(context.Context, string) (packages.Info, error) {
			t.Fatal("inspected before successful resolution")
			return packages.Info{}, nil
		}))
		if err == nil || len(got.Entries) != 0 {
			t.Fatalf("plan %+v; error %v", got, err)
		}
	}
}

func TestSharedObservationAndContinue(t *testing.T) {
	failure := errors.New("cannot inspect")
	catalog := software.Catalog{
		"one": fixture("one", "shared"), "two": fixture("two", "shared"), "other": fixture("other", "other"),
	}
	for _, inspectionFails := range []bool{false, true} {
		var calls []string
		got, err := Build(context.Background(), catalog, software.Desired{IDs: []string{"one", "two", "other"}, Provider: software.APT},
			inspectorFunc(func(_ context.Context, name string) (packages.Info, error) {
				calls = append(calls, name)
				info := packages.Info{Name: name, Status: packages.Available, CandidateVersion: "1"}
				if name == "shared" && inspectionFails {
					return info, failure
				}
				return info, nil
			}))
		if !reflect.DeepEqual(calls, []string{"shared", "other"}) || len(got.Entries) != 3 {
			t.Fatalf("calls %v; plan %+v", calls, got)
		}
		if got.Entries[0].Current != got.Entries[1].Current || got.Entries[0].Action != got.Entries[1].Action || got.Entries[2].Action != Install {
			t.Fatalf("inconsistent cached observation or continuation: %+v", got)
		}
		if inspectionFails {
			if !errors.Is(err, failure) || !strings.Contains(err.Error(), "one") || !strings.Contains(err.Error(), "two") {
				t.Fatalf("missing logical error contexts: %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

func TestInspectionNameMismatch(t *testing.T) {
	got, err := Build(context.Background(), software.Catalog{"app": fixture("app", "pkg")},
		software.Desired{IDs: []string{"app"}, Provider: software.APT},
		inspectorFunc(func(context.Context, string) (packages.Info, error) {
			return packages.Info{Name: "different", Status: packages.Available, CandidateVersion: "1"}, nil
		}))
	if err == nil || got.Entries[0].Action != Error || !strings.Contains(err.Error(), "different") {
		t.Fatalf("plan %+v; error %v", got, err)
	}
}

func TestContextHandling(t *testing.T) {
	type contextKey struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "marker"))
	defer cancel()
	calls := 0
	got, err := Build(ctx, software.Catalog{"one": fixture("one", "one"), "two": fixture("two", "two")},
		software.Desired{IDs: []string{"one", "two"}, Provider: software.APT},
		inspectorFunc(func(received context.Context, name string) (packages.Info, error) {
			calls++
			if received != ctx || received.Value(contextKey{}) != "marker" {
				t.Fatal("context not forwarded")
			}
			cancel()
			return packages.Info{Name: name}, received.Err()
		}))
	if calls != 1 || len(got.Entries) != 2 || !errors.Is(err, context.Canceled) {
		t.Fatalf("calls %d; plan %+v; error %v", calls, got, err)
	}
	for _, entry := range got.Entries {
		if entry.Action != Error || !errors.Is(entry.Err, context.Canceled) {
			t.Fatalf("entry = %+v", entry)
		}
	}
}

func TestAlreadyCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := Build(ctx, software.BuiltinCatalog(), software.Desired{IDs: []string{"git", "bash"}, Provider: software.APT},
		inspectorFunc(func(context.Context, string) (packages.Info, error) {
			t.Fatal("inspected with canceled context")
			return packages.Info{}, nil
		}))
	if len(got.Entries) != 2 || !errors.Is(err, context.Canceled) {
		t.Fatalf("plan %+v; error %v", got, err)
	}
}

func TestErrorOrderAndRepeatDeterminism(t *testing.T) {
	catalog := software.Catalog{"first": fixture("first", "first"), "second": fixture("second", "second")}
	var previous string
	for run := 0; run < 3; run++ {
		got, err := Build(context.Background(), catalog, software.Desired{IDs: []string{"first", "second"}, Provider: software.APT},
			inspectorFunc(func(_ context.Context, name string) (packages.Info, error) {
				return packages.Info{Name: name, Status: packages.Unavailable}, nil
			}))
		if err == nil || len(got.Entries) != 2 || !errors.Is(err, ErrUnavailable) {
			t.Fatalf("plan %+v; error %v", got, err)
		}
		message := err.Error()
		if strings.Index(message, `software "first"`) >= strings.Index(message, `software "second"`) {
			t.Fatalf("incorrect error order: %s", message)
		}
		if run > 0 && message != previous {
			t.Fatalf("non-deterministic errors: %q vs %q", message, previous)
		}
		previous = message
	}
}

func TestEmptyAndNilInspector(t *testing.T) {
	got, err := Build(context.Background(), nil, software.Desired{Provider: software.APT}, inspectorFunc(func(context.Context, string) (packages.Info, error) {
		t.Fatal("empty selection inspected")
		return packages.Info{}, nil
	}))
	if err != nil || len(got.Entries) != 0 {
		t.Fatalf("plan %+v; error %v", got, err)
	}
	got, err = Build(context.Background(), software.BuiltinCatalog(), software.Desired{IDs: []string{"git"}, Provider: software.APT}, nil)
	if err == nil || len(got.Entries) != 0 || !strings.Contains(err.Error(), "inspector") {
		t.Fatalf("plan %+v; error %v", got, err)
	}
}
