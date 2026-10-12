package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/Codenburg/Codenburg-dots/internal/packages"
	"github.com/Codenburg/Codenburg-dots/internal/providers/flatpak"
)

type flatpakFake struct {
	evidence   flatpak.Evidence
	inventory  flatpak.Inventory
	err        error
	selections []flatpak.Selection
	scopes     []flatpak.Scope
}

func (f *flatpakFake) Inspect(ctx context.Context, s flatpak.Selection) (flatpak.Evidence, error) {
	f.selections = append(f.selections, s)
	if err := ctx.Err(); err != nil {
		return flatpak.Evidence{}, err
	}
	return f.evidence, f.err
}
func (f *flatpakFake) Inventory(ctx context.Context, s flatpak.Scope) (flatpak.Inventory, error) {
	f.scopes = append(f.scopes, s)
	if err := ctx.Err(); err != nil {
		return flatpak.Inventory{}, err
	}
	return f.inventory, f.err
}
func firefoxRef(scope flatpak.Scope) flatpak.Ref {
	return flatpak.Ref{ID: "org.mozilla.firefox", Scope: scope, Remote: "flathub", Arch: "x86_64", Branch: "stable"}
}

func TestFlatpakPlansDeterministicAndSeparateFromAPT(t *testing.T) {
	f := &flatpakFake{evidence: flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{firefoxRef(flatpak.User)}}}
	d := &planDetector{}
	apt := &planInspector{}
	args := []string{"plan", "--provider=flatpak", "--scope=user", "--arch=x86_64", "firefox"}
	var previous string
	for i := 0; i < 3; i++ {
		var out bytes.Buffer
		err := RunReadOnly(context.Background(), args, &out, io.Discard, d, apt, func() (FlatpakInspector, error) { return f, nil })
		if err != nil {
			t.Fatal(err)
		}
		text := out.String()
		for _, want := range []string{"Provider: flatpak", "Requested: id=org.mozilla.firefox scope=user remote=flathub arch=x86_64 branch=stable", "Installed ref: scope=user id=org.mozilla.firefox arch=x86_64 branch=stable origin=flathub version=unknown", "Candidate (cached; freshness/installability unknown) refs: none observed", "Action: none"} {
			if !strings.Contains(text, want) {
				t.Fatalf("missing %q in %q", want, text)
			}
		}
		if i > 0 && text != previous {
			t.Fatal("plan changed across repeats")
		}
		previous = text
	}
	if d.calls != 0 || len(apt.calls) != 0 || len(f.selections) != 3 {
		t.Fatal("Flatpak crossed the APT boundary")
	}
	var out bytes.Buffer
	apt.states = map[string]PackageInfo{"firefox-esr": {Name: "firefox-esr", Status: packages.Current, InstalledVersion: "123", CandidateVersion: "123"}}
	err := RunReadOnly(context.Background(), []string{"plan", "--provider=apt", "firefox"}, &out, io.Discard, fakeSystem{}, apt, func() (FlatpakInspector, error) { t.Fatal("APT created Flatpak"); return nil, nil })
	if err != nil || !reflect.DeepEqual(apt.calls, []string{"firefox-esr"}) || !strings.Contains(out.String(), "Provider: apt\nPackage: firefox-esr") {
		t.Fatalf("explicit APT: %v %q", err, out.String())
	}
}

func TestFlatpakEmptyInventoryReportsObservationState(t *testing.T) {
	var out bytes.Buffer
	f := &flatpakFake{}
	err := RunReadOnly(context.Background(), []string{"flatpak", "--scope=user"}, &out, io.Discard, nil, nil, func() (FlatpakInspector, error) { return f, nil })
	if err != nil || !strings.Contains(out.String(), "Remotes: none observed") || !strings.Contains(out.String(), "State: complete local inventory") {
		t.Fatalf("empty inventory state missing: %v %q", err, out.String())
	}
	f.inventory.Diagnostics = []flatpak.Diagnostic{{Scope: flatpak.User, Operation: flatpak.List, Err: errors.New("incomplete")}}
	out.Reset()
	err = RunReadOnly(context.Background(), []string{"flatpak", "--scope=user"}, &out, io.Discard, nil, nil, func() (FlatpakInspector, error) { return f, nil })
	if err == nil || !strings.Contains(out.String(), "State: incomplete local inventory") {
		t.Fatalf("partial inventory state missing: %v %q", err, out.String())
	}
}

func TestFlatpakInventoryScopesAndRemoteState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		scopes []flatpak.Scope
	}{
		{name: "default", args: []string{"flatpak"}, scopes: []flatpak.Scope{flatpak.User, flatpak.System}},
		{name: "named", args: []string{"flatpak", "--scope", "installation:work"}, scopes: []flatpak.Scope{"installation:work"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &flatpakFake{inventory: flatpak.Inventory{Remotes: []flatpak.Remote{{Scope: flatpak.User, Name: "flathub", Disabled: true}}}}
			var out bytes.Buffer
			err := RunReadOnly(context.Background(), tc.args, &out, io.Discard, nil, nil, func() (FlatpakInspector, error) { return f, nil })
			if err != nil || !reflect.DeepEqual(f.scopes, tc.scopes) || !strings.Contains(out.String(), "name=flathub disabled=true") {
				t.Fatalf("inventory: %v %v %q", err, f.scopes, out.String())
			}
		})
	}
}

func TestFlatpakEvidenceFailuresRetainRowsAndDiagnostics(t *testing.T) {
	failure := errors.New("malformed installed row")
	for _, tc := range []struct {
		name   string
		e      flatpak.Evidence
		err    error
		action string
	}{
		{name: "unavailable", e: flatpak.Evidence{State: flatpak.Unavailable}, action: "unavailable"},
		{name: "ambiguous user/system", e: flatpak.Evidence{State: flatpak.Ambiguous, Installed: []flatpak.Ref{firefoxRef(flatpak.System), firefoxRef(flatpak.User)}}, action: "ambiguous"},
		{name: "incomplete list", e: flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{firefoxRef(flatpak.User)}, Diagnostics: []flatpak.Diagnostic{{Scope: flatpak.System, Operation: flatpak.List, Err: failure}}}, action: "error"},
		{name: "satisfied with remote diagnostic", e: flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{firefoxRef(flatpak.User)}, Diagnostics: []flatpak.Diagnostic{{Scope: flatpak.System, Operation: flatpak.RemoteLS, Remote: "flathub", Err: failure}}}, action: "none"},
		{name: "query failure", err: failure, action: "error"},
		{name: "malformed ref", e: flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{{ID: "org.mozilla.firefox"}}}, action: "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &flatpakFake{evidence: tc.e, err: tc.err}
			var out bytes.Buffer
			err := RunReadOnly(context.Background(), []string{"plan", "--provider=flatpak", "firefox"}, &out, io.Discard, nil, nil, func() (FlatpakInspector, error) { return f, nil })
			if err == nil || !strings.Contains(out.String(), "Action: "+tc.action+"\n") || !strings.Contains(out.String(), "Diagnostic:") {
				t.Fatalf("plan hid failure: %v %q", err, out.String())
			}
			if len(tc.e.Installed) > 0 && !strings.Contains(out.String(), "Installed ref:") {
				t.Fatal("valid observation hidden")
			}
		})
	}
}

func TestFlatpakCachedCandidateVersionIsIndependent(t *testing.T) {
	installed := firefoxRef(flatpak.User)
	installed.Version = "installed-opaque"
	candidate := firefoxRef(flatpak.User)
	candidate.Version = "cached-opaque"
	f := &flatpakFake{evidence: flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{installed}, Candidates: []flatpak.Ref{candidate}}}
	var out bytes.Buffer
	err := RunReadOnly(context.Background(), []string{"plan", "--provider=flatpak", "firefox"}, &out, io.Discard, nil, nil, func() (FlatpakInspector, error) { return f, nil })
	if err != nil || !strings.Contains(out.String(), "origin=flathub version=installed-opaque") || !strings.Contains(out.String(), "Candidate (cached; freshness/installability unknown) ref: scope=user id=org.mozilla.firefox arch=x86_64 branch=stable origin=flathub version=cached-opaque") || !strings.Contains(out.String(), "Action: none") {
		t.Fatalf("version evidence conflated: %v %q", err, out.String())
	}
	f.evidence = flatpak.Evidence{State: flatpak.Available, Candidates: []flatpak.Ref{candidate}}
	out.Reset()
	err = RunReadOnly(context.Background(), []string{"plan", "--provider=flatpak", "firefox"}, &out, io.Discard, nil, nil, func() (FlatpakInspector, error) { return f, nil })
	if err != nil || !strings.Contains(out.String(), "Action: install") {
		t.Fatalf("available plan: %v %q", err, out.String())
	}
}

func TestFlatpakRawFailureStatesAndInventoryDiagnostics(t *testing.T) {
	for _, state := range []flatpak.State{flatpak.Unknown, flatpak.Unavailable, flatpak.Ambiguous, "malformed"} {
		t.Run(string(state), func(t *testing.T) {
			f := &flatpakFake{evidence: flatpak.Evidence{State: state}}
			var out bytes.Buffer
			err := RunReadOnly(context.Background(), []string{"flatpak", "org.mozilla.firefox"}, &out, io.Discard, nil, nil, func() (FlatpakInspector, error) { return f, nil })
			if err == nil || !strings.Contains(out.String(), "State: "+string(state)) || !strings.Contains(out.String(), "Diagnostic:") {
				t.Fatalf("raw failure hidden: %v %q", err, out.String())
			}
		})
	}
	failure := errors.New("partial list")
	f := &flatpakFake{inventory: flatpak.Inventory{Installed: []flatpak.Ref{firefoxRef(flatpak.User)}, Diagnostics: []flatpak.Diagnostic{{Scope: flatpak.User, Operation: flatpak.List, Err: failure}}}}
	var out bytes.Buffer
	err := RunReadOnly(context.Background(), []string{"flatpak", "--scope=user"}, &out, io.Discard, nil, nil, func() (FlatpakInspector, error) { return f, nil })
	if !errors.Is(err, failure) || !strings.Contains(out.String(), "Installed ref:") || !strings.Contains(out.String(), "partial list") {
		t.Fatalf("partial inventory hidden: %v %q", err, out.String())
	}
}

func TestFlatpakInspectionFiltersAndPartialUniqueness(t *testing.T) {
	failure := errors.New("list is incomplete")
	f := &flatpakFake{evidence: flatpak.Evidence{State: flatpak.Installed, Installed: []flatpak.Ref{firefoxRef(flatpak.User)}, Candidates: []flatpak.Ref{firefoxRef(flatpak.System)}, Diagnostics: []flatpak.Diagnostic{{Scope: flatpak.System, Operation: flatpak.List, Err: failure}}}}
	var out bytes.Buffer
	err := RunReadOnly(context.Background(), []string{"flatpak", "--remote", "flathub", "--arch=x86_64", "--branch=stable", "org.mozilla.firefox"}, &out, io.Discard, nil, nil, func() (FlatpakInspector, error) { return f, nil })
	want := flatpak.Selection{ID: "org.mozilla.firefox", Remote: "flathub", Arch: "x86_64", Branch: "stable"}
	if !errors.Is(err, failure) || !reflect.DeepEqual(f.selections, []flatpak.Selection{want}) {
		t.Fatalf("inspect filters/errors: %v %v", err, f.selections)
	}
	for _, text := range []string{"incomplete lists cannot prove uniqueness", "list is incomplete", "origin=flathub version=unknown", "Candidate (cached; freshness/installability unknown) ref: scope=system"} {
		if !strings.Contains(out.String(), text) {
			t.Fatalf("missing %q: %q", text, out.String())
		}
	}
}

type flatpakBrokenWriter struct{ err error }

func (w flatpakBrokenWriter) Write([]byte) (int, error) { return 0, w.err }

func TestFlatpakMissingToolsWriteErrorsAndCancellation(t *testing.T) {
	failure := errors.New("flatpak/bwrap missing or sandbox unavailable")
	var out bytes.Buffer
	err := RunReadOnly(context.Background(), []string{"flatpak"}, &out, io.Discard, nil, nil, func() (FlatpakInspector, error) { return nil, failure })
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "unavailable") || out.Len() != 0 {
		t.Fatalf("missing tool: %v %q", err, out.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = RunReadOnly(ctx, []string{"plan", "--provider=flatpak", "firefox"}, &out, io.Discard, nil, nil, func() (FlatpakInspector, error) { t.Fatal("cancelled factory called"); return nil, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
	for _, args := range [][]string{{"flatpak"}, {"flatpak", "org.mozilla.firefox"}, {"plan", "--provider=flatpak", "firefox"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := &flatpakFake{evidence: flatpak.Evidence{State: flatpak.Available, Candidates: []flatpak.Ref{firefoxRef(flatpak.User)}}}
			err := RunReadOnly(context.Background(), args, flatpakBrokenWriter{err: failure}, io.Discard, nil, nil, func() (FlatpakInspector, error) { return f, nil })
			if !errors.Is(err, failure) {
				t.Fatalf("write error hidden: %v", err)
			}
		})
	}
}

func TestReadOnlyNeverRoutesApplyToFlatpak(t *testing.T) {
	for _, args := range [][]string{{"apply", "firefox"}, {"apply", "--provider=flatpak", "firefox"}} {
		var out bytes.Buffer
		err := RunReadOnly(context.Background(), args, &out, io.Discard, nil, nil, func() (FlatpakInspector, error) { t.Fatal("apply reached Flatpak"); return nil, nil })
		if err == nil || !strings.Contains(err.Error(), "interactive entry point") {
			t.Fatalf("apply route changed: %v", err)
		}
	}
}

func TestPlanRequiresProviderBeforeHostChecks(t *testing.T) {
	d := &planDetector{}
	p := &planInspector{}
	var out bytes.Buffer
	err := Run([]string{"plan", "firefox"}, &out, &out, d, p)
	if err == nil || !strings.Contains(err.Error(), "explicit --provider") || d.calls != 0 || len(p.calls) != 0 || out.Len() != 0 {
		t.Fatalf("provider choice must precede host work: %v detections=%d queries=%v output=%q", err, d.calls, p.calls, out.String())
	}
}

func TestReadOnlyInvalidSelectionBeforeFactory(t *testing.T) {
	for _, args := range [][]string{
		{"plan", "firefox"}, {"plan", "--provider=unknown", "firefox"},
		{"plan", "--provider=apt", "--scope=user", "firefox"},
		{"plan", "--provider=flatpak", "--provider=flatpak", "firefox"},
		{"plan", "--provider=flatpak", "firefox", "--arch=x86_64"},
		{"plan", "--provider=flatpak", "missing"},
		{"flatpak", "--remote=flathub"}, {"flatpak", "--scope=bad"},
		{"flatpak", "--branch="}, {"flatpak", "--provider=flatpak"},
		{"flatpak", "bad/id"}, {"flatpak", "org.mozilla.firefox", "extra"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			calls := 0
			d := &planDetector{}
			var out bytes.Buffer
			err := RunReadOnly(context.Background(), args, &out, &out, d, nil, func() (FlatpakInspector, error) { calls++; return nil, nil })
			if err == nil || calls != 0 || d.calls != 0 || out.Len() != 0 {
				t.Fatalf("unexpected work: %v %d %d %q", err, calls, d.calls, out.String())
			}
		})
	}
}
