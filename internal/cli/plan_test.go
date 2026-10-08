package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Codenburg/Codenburg-dots/internal/packages"
)

type planDetector struct {
	info  SystemInfo
	err   error
	calls int
}

func (d *planDetector) Detect() (SystemInfo, error) {
	d.calls++
	return d.info, d.err
}

type planInspector struct {
	states map[string]PackageInfo
	errs   map[string]error
	calls  []string
}

func (p *planInspector) Inspect(_ context.Context, name string) (PackageInfo, error) {
	p.calls = append(p.calls, name)
	return p.states[name], p.errs[name]
}

func TestPlanOrderedDeduplicatedAndReadOnly(t *testing.T) {
	const want = "Software: bash (Bash)\nProvider: apt\nPackage: bash\nDesired: present\nInstalled version: 1\nCandidate version: 2\nAction: none\n\n" +
		"Software: git (Git)\nProvider: apt\nPackage: git\nDesired: present\nInstalled version: unknown\nCandidate version: 3\nAction: install\n\n" +
		"Software: neovim (Neovim)\nProvider: apt\nPackage: neovim\nDesired: present\nInstalled version: 4\nCandidate version: unknown\nAction: none\n"
	for i := 0; i < 3; i++ {
		inspector := &planInspector{states: map[string]PackageInfo{
			"bash":   {Name: "bash", Status: packages.UpdateAvailable, InstalledVersion: "1", CandidateVersion: "2"},
			"git":    {Name: "git", Status: packages.Available, CandidateVersion: "3"},
			"neovim": {Name: "neovim", Status: packages.Unknown, InstalledVersion: "4"},
		}}
		var out, stderr bytes.Buffer
		err := Run([]string{"plan", "bash", "git", "bash", "neovim", "git"}, &out, &stderr, fakeSystem{}, inspector)
		if err != nil {
			t.Fatal(err)
		}
		if out.String() != want {
			t.Fatalf("output:\n%s\nwant:\n%s", out.String(), want)
		}
		if !reflect.DeepEqual(inspector.calls, []string{"bash", "git", "neovim"}) {
			t.Fatalf("inspection order/dedup: %v", inspector.calls)
		}
		if stderr.Len() != 0 {
			t.Fatalf("unexpected stderr: %q", stderr.String())
		}
	}
}

func TestPlanRetainsUnavailableAndInspectionErrorEntries(t *testing.T) {
	failure := errors.New("injected APT query failure")
	inspector := &planInspector{
		states: map[string]PackageInfo{
			"bash": {Name: "bash", Status: packages.Current, InstalledVersion: "1", CandidateVersion: "1"},
			"git":  {Name: "git", Status: packages.Unavailable},
		},
		errs: map[string]error{"neovim": failure},
	}
	var out, stderr bytes.Buffer
	err := Run([]string{"plan", "bash", "git", "neovim"}, &out, &stderr, fakeSystem{}, inspector)
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "package unavailable") {
		t.Fatalf("incomplete plan errors hidden: %v", err)
	}
	for _, diagnostic := range []string{
		`software "git" provider "apt" package "git": package unavailable`,
		`software "neovim" provider "apt" package "neovim": injected APT query failure`,
	} {
		if !strings.Contains(err.Error(), diagnostic) || !strings.Contains(out.String(), "Diagnostic: "+diagnostic) {
			t.Fatalf("missing contextual diagnostic %q; output: %q; error: %v", diagnostic, out.String(), err)
		}
	}
	text := out.String()
	previous := -1
	for _, block := range []struct{ id, action string }{{"bash", "none"}, {"git", "unavailable"}, {"neovim", "error"}} {
		start := strings.Index(text, "Software: "+block.id)
		if start <= previous {
			t.Fatalf("missing or reordered entry %s: %q", block.id, text)
		}
		end := strings.Index(text[start:], "\n\n")
		entry := text[start:]
		if end >= 0 {
			entry = entry[:end]
		}
		if !strings.Contains(entry+"\n", "\nAction: "+block.action+"\n") {
			t.Fatalf("wrong action for %s: %q", block.id, entry)
		}
		previous = start
	}
	if !reflect.DeepEqual(inspector.calls, []string{"bash", "git", "neovim"}) {
		t.Fatalf("incomplete plan stopped early: %v", inspector.calls)
	}
}

func TestPlanRetainsMixedInstalledAvailableAndUnavailableEntries(t *testing.T) {
	// Injected observation-only fixtures keep this mixed plan isolated from the host.
	inspector := &planInspector{states: map[string]PackageInfo{
		"bash":   {Name: "bash", Status: packages.Current, InstalledVersion: "1", CandidateVersion: "1"},
		"git":    {Name: "git", Status: packages.Unavailable},
		"neovim": {Name: "neovim", Status: packages.Available, CandidateVersion: "3"},
	}}
	var out, stderr bytes.Buffer
	err := Run([]string{"plan", "bash", "git", "neovim"}, &out, &stderr, fakeSystem{}, inspector)
	const diagnostic = `software "git" provider "apt" package "git": package unavailable`
	if err == nil || !strings.Contains(err.Error(), diagnostic) {
		t.Fatalf("missing contextual unavailable error: %v", err)
	}
	const want = "Software: bash (Bash)\nProvider: apt\nPackage: bash\nDesired: present\nInstalled version: 1\nCandidate version: 1\nAction: none\n\n" +
		"Software: git (Git)\nProvider: apt\nPackage: git\nDesired: present\nInstalled version: unknown\nCandidate version: unknown\nAction: unavailable\nDiagnostic: " + diagnostic + "\n\n" +
		"Software: neovim (Neovim)\nProvider: apt\nPackage: neovim\nDesired: present\nInstalled version: unknown\nCandidate version: 3\nAction: install\n"
	if out.String() != want {
		t.Fatalf("mixed plan output:\n%s\nwant:\n%s", out.String(), want)
	}
	if !reflect.DeepEqual(inspector.calls, []string{"bash", "git", "neovim"}) {
		t.Fatalf("mixed plan stopped early or reordered queries: %v", inspector.calls)
	}
	if stderr.Len() != 0 {
		t.Fatalf("Run should return diagnostics for main to report: %q", stderr.String())
	}
}

func TestPlanValidationBeforeAnyInspection(t *testing.T) {
	for _, tc := range []struct {
		name           string
		args           []string
		info           SystemInfo
		detectErr      error
		nilInspector   bool
		want           string
		wantDetections int
	}{
		{name: "missing args", args: []string{"plan"}, want: "usage: cdots plan <software-id>..."},
		{name: "unknown ID before unsupported OS", args: []string{"plan", "bash", "nonexistent"}, info: SystemInfo{ID: "arch"}, want: `unknown software ID "nonexistent"`},
		{name: "invalid ID", args: []string{"plan", "bash", "bad/id"}, want: `invalid software ID "bad/id"`},
		{name: "unknown ID without inspector", args: []string{"plan", "nonexistent"}, nilInspector: true, want: `unknown software ID "nonexistent"`},
		{name: "unsupported OS", args: []string{"plan", "bash"}, info: SystemInfo{ID: "arch", IDLike: "debian", APTAvailable: true}, want: `unsupported distribution "arch" (ID_LIKE: debian)`, wantDetections: 1},
		{name: "missing APT", args: []string{"plan", "git"}, info: SystemInfo{ID: "ubuntu", Supported: true}, want: "APT inspection requires dpkg-query, apt-cache, and dpkg in PATH", wantDetections: 1},
		{name: "detection failure", args: []string{"plan", "bash"}, detectErr: errors.New("fixture detection failure"), want: "detect system: fixture detection failure", wantDetections: 1},
		{name: "no inspector", args: []string{"plan", "bash"}, info: SystemInfo{Supported: true, APTAvailable: true}, nilInspector: true, want: "APT package inspection is unavailable", wantDetections: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detector := &planDetector{info: tc.info, err: tc.detectErr}
			spy := &planInspector{}
			var inspector PackageInspector = spy
			if tc.nilInspector {
				inspector = nil
			}
			var out, stderr bytes.Buffer
			err := Run(tc.args, &out, &stderr, detector, inspector)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			if out.Len() != 0 || len(spy.calls) != 0 || detector.calls != tc.wantDetections {
				t.Fatalf("unexpected work: output %q, queries %v, detections %d", out.String(), spy.calls, detector.calls)
			}
		})
	}
}

func TestPhaseOnePackageEnvironmentGuards(t *testing.T) {
	for _, tc := range []struct {
		info SystemInfo
		want string
	}{
		{SystemInfo{ID: "arch", APTAvailable: true}, "unsupported distribution"},
		{SystemInfo{ID: "ubuntu", Supported: true}, "APT inspection requires"},
	} {
		var out, stderr bytes.Buffer
		spy := &planInspector{}
		err := Run([]string{"package", "bash"}, &out, &stderr, &planDetector{info: tc.info}, spy)
		if err == nil || !strings.Contains(err.Error(), tc.want) || len(spy.calls) != 0 || out.Len() != 0 {
			t.Fatalf("Phase 1 guard changed: error %v, queries %v, output %q", err, spy.calls, out.String())
		}
	}
}

func TestUsageIncludesPlan(t *testing.T) {
	var out, stderr bytes.Buffer
	err := Run([]string{"not-a-command"}, &out, &stderr, fakeSystem{}, fakePackages{})
	if err == nil || !strings.Contains(err.Error(), "plan <software-id>...") {
		t.Fatalf("plan missing from usage: %v", err)
	}
}
