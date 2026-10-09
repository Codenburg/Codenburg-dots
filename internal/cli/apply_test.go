package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Codenburg/Codenburg-dots/internal/apply"
	"github.com/Codenburg/Codenburg-dots/internal/packages"
	"github.com/Codenburg/Codenburg-dots/internal/providers/apt"
	"github.com/Codenburg/Codenburg-dots/internal/software"
)

type applyBackend struct {
	installed     bool
	preview       apt.Preview
	validation    error
	simulationErr error
	drift         bool
	simulations   int
	operands      []string
	post          map[string]packages.Info
}

func (b *applyBackend) Inspect(ctx context.Context, name string) (packages.Info, error) {
	return b.InspectApply(ctx, name)
}
func (b *applyBackend) InspectApply(_ context.Context, name string) (packages.Info, error) {
	if b.post != nil {
		if p, ok := b.post[name]; ok {
			return p, nil
		}
	}
	p := packages.Info{Name: name, Status: packages.Available, CandidateVersion: "1"}
	if b.installed {
		p.Status = packages.Current
		p.InstalledVersion = "1"
	}
	return p, nil
}
func (b *applyBackend) InspectApplyState(ctx context.Context, name string) (apt.ApplyState, error) {
	p, err := b.InspectApply(ctx, name)
	base, _, _ := strings.Cut(name, ":")
	s := apt.ApplyState{Info: p, Candidate: apt.PackageEvidence{Known: true, Package: base, Architecture: "amd64", Version: "1"}}
	if p.InstalledVersion != "" {
		s.Installed = apt.PackageEvidence{Known: true, Package: base, Architecture: "amd64", Version: p.InstalledVersion, Status: "install ok installed"}
	}
	return s, err
}
func (b *applyBackend) Simulate(_ context.Context, names []string) (apt.Preview, error) {
	b.simulations++
	b.operands = append([]string{}, names...)
	p := b.preview
	p.Requests = append([]string{}, names...)
	p.Operations = append([]apt.Operation{}, p.Operations...)
	if b.drift && b.simulations > 1 {
		p.Operations[0].NewVersion = "2"
	}
	return p, b.simulationErr
}
func (b *applyBackend) ValidateApply(context.Context, apt.Preview) error { return b.validation }

type applyExecutor struct {
	b        *applyBackend
	calls    int
	err      error
	noChange bool
	run      func(context.Context) error
}

func (e *applyExecutor) Execute(ctx context.Context, _ apt.Preview) error {
	e.calls++
	if e.run != nil {
		return e.run(ctx)
	}
	if !e.noChange && e.err == nil {
		e.b.installed = true
	}
	return e.err
}
func applyFixture() (*applyBackend, *applyExecutor) {
	b := &applyBackend{preview: apt.Preview{Operations: []apt.Operation{{Kind: "install", Package: "git", Architecture: "amd64", NewVersion: "1", Risk: "normal", Candidate: apt.PackageEvidence{Known: true, Package: "git", Architecture: "amd64", Version: "1"}}}}}
	return b, &applyExecutor{b: b}
}

// Only this package-private path supplies synthetic terminal capability. No real
// executor, provider subprocess or shipped capability switch is used.
func invokeApply(ctx context.Context, b *applyBackend, e *applyExecutor, answers string, tty bool) (string, error) {
	var out bytes.Buffer
	reader := strings.NewReader(answers)
	r := &terminalReviewer{out: &out, capable: func() bool { return tty }, readLine: func(ctx context.Context) (string, error) { return readScalarLine(ctx, reader) }}
	err := runApply(ctx, []string{"git", "git"}, &out, fakeSystem{}, b, software.BuiltinCatalog(), e, r)
	return out.String(), err
}
func TestApplyOrdinaryNoopAndRefusal(t *testing.T) {
	for _, tc := range []struct {
		name, answer   string
		tty, installed bool
		status         string
		calls          int
	}{
		{name: "ordinary", answer: "apply\n", tty: true, status: "success", calls: 1},
		{name: "noop noninteractive", installed: true, status: "no-action"},
		{name: "no tty", answer: "apply\n", status: "failure"},
		{name: "empty", answer: "\n", tty: true, status: "cancellation"},
		{name: "no", answer: "no\n", tty: true, status: "cancellation"},
		{name: "EOF", tty: true, status: "cancellation"},
		{name: "unterminated", answer: "apply", tty: true, status: "cancellation"},
		{name: "generic yes", answer: "yes\n", tty: true, status: "cancellation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, e := applyFixture()
			b.installed = tc.installed
			out, err := invokeApply(context.Background(), b, e, tc.answer, tc.tty)
			if e.calls != tc.calls || !strings.Contains(out, "Apply result: "+tc.status) {
				t.Fatalf("calls=%d err=%v out=%s", e.calls, err, out)
			}
			if tc.status == "success" || tc.status == "no-action" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("missing refusal")
			}
			if b.simulations > 0 && strings.Join(b.operands, ",") != "git" {
				t.Fatal(b.operands)
			}
			if tc.installed && (strings.Contains(out, "Type ") || b.simulations != 0 || !strings.Contains(out, "no reinstall or automatic upgrade")) {
				t.Fatal(out)
			}
			if tc.status == "success" && (strings.Contains(out, "No changes applied") || strings.Contains(out, "Execution is not implemented")) {
				t.Fatal(out)
			}
		})
	}
}
func TestApplyCompleteRenderAndEachBoundary(t *testing.T) {
	upgrade := apt.Operation{Kind: "upgrade", Package: "libgit", Architecture: "amd64", OldVersion: "1", NewVersion: "2", Risk: "review-required", Installed: apt.PackageEvidence{Known: true, Package: "libgit", Architecture: "amd64", Version: "1", Status: "install ok installed"}, Candidate: apt.PackageEvidence{Known: true, Package: "libgit", Architecture: "amd64", Version: "2"}}
	remove := apt.Operation{Kind: "removal", Package: "obsolete", Architecture: "amd64", OldVersion: "3", Risk: "high", Installed: apt.PackageEvidence{Known: true, Package: "obsolete", Architecture: "amd64", Version: "3", Status: "install ok installed"}}
	for _, tc := range []struct {
		name, answer string
		calls        int
	}{
		{"accepted", "review upgrade libgit:amd64\nremove obsolete:amd64\napply\n", 1},
		{"upgrade no", "no\n", 0},
		{"upgrade EOF", "", 0},
		{"remove no", "review upgrade libgit:amd64\nno\n", 0},
		{"remove EOF", "review upgrade libgit:amd64\n", 0},
		{"remove generic", "review upgrade libgit:amd64\napply\n", 0},
		{"remove wrong arch", "review upgrade libgit:amd64\nremove obsolete:arm64\n", 0},
		{"general no", "review upgrade libgit:amd64\nremove obsolete:amd64\nno\n", 0},
		{"general EOF", "review upgrade libgit:amd64\nremove obsolete:amd64\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, e := applyFixture()
			b.preview.Operations = append(b.preview.Operations, upgrade, remove)
			b.preview.Warnings = []string{"snapshot warning"}
			e.run = func(context.Context) error {
				b.installed = true
				b.post = map[string]packages.Info{"libgit:amd64": {Name: "libgit:amd64", Status: packages.Current, InstalledVersion: "2"}, "obsolete:amd64": {Name: "obsolete:amd64", Status: packages.Unavailable}}
				return nil
			}
			out, err := invokeApply(context.Background(), b, e, tc.answer, true)
			if e.calls != tc.calls {
				t.Fatalf("calls=%d %v %s", e.calls, err, out)
			}
			if tc.calls == 1 && err != nil {
				t.Fatal(err)
			}
			if tc.calls == 0 && !errors.Is(err, apply.ErrRejected) {
				t.Fatalf("not refusal: %v", err)
			}
			question := strings.Index(out, "Type ")
			for _, text := range []string{"Requested software: git", "Provider: apt", "Resolved package: git", "requested software git", "Operation: upgrade", "Operation: removal", "Old version: 3", "Installed evidence:", "known=true", "held=false", "snapshot warning", "APT 2.6.1", "no automatic elevation"} {
				i := strings.Index(out, text)
				if i < 0 || i > question {
					t.Fatalf("missing before prompt %q: %s", text, out)
				}
			}
		})
	}
}
func TestApplyBlockedEvidenceAndExecutionReports(t *testing.T) {
	for _, tc := range []struct {
		name                string
		validation, execErr error
		drift, noChange     bool
		want                string
	}{
		{name: "essential removal", validation: errors.New("Essential removal forbidden"), want: "failure"},
		{name: "protected removal", validation: errors.New("Protected removal forbidden"), want: "failure"},
		{name: "unknown metadata", validation: errors.New("unknown metadata"), want: "failure"},
		{name: "held", validation: errors.New("held package"), want: "failure"},
		{name: "downgrade", validation: errors.New("downgrade forbidden"), want: "failure"},
		{name: "drift", drift: true, want: "failure"},
		{name: "privilege", execErr: errors.New("root required; no automatic elevation"), want: "failure"},
		{name: "native failure", execErr: errors.New("exit status 100: native stderr"), want: "failure"},
		{name: "exit0 no state", noChange: true, want: "failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, e := applyFixture()
			b.validation = tc.validation
			b.drift = tc.drift
			e.err = tc.execErr
			e.noChange = tc.noChange
			out, err := invokeApply(context.Background(), b, e, "apply\n", true)
			if err == nil || !strings.Contains(out, "Apply result: "+tc.want) {
				t.Fatalf("%v %s", err, out)
			}
			if tc.validation != nil && (e.calls != 0 || strings.Contains(out, "Type ") || !strings.Contains(out, "Resolved package: git")) {
				t.Fatal(out)
			}
			if tc.drift && (e.calls != 0 || !strings.Contains(out, "fresh review required")) {
				t.Fatal(out)
			}
			if tc.execErr != nil && !strings.Contains(out, tc.execErr.Error()) {
				t.Fatal(out)
			}
		})
	}
}
func TestApplyRepeatFreshAndNoReadAhead(t *testing.T) {
	for i := 0; i < 2; i++ {
		b, e := applyFixture()
		out, err := invokeApply(context.Background(), b, e, "apply\n", true)
		if err != nil || strings.Count(out, "Type \"apply\"") != 1 {
			t.Fatalf("%v %s", err, out)
		}
	}
	input := strings.NewReader("apply\ny\n")
	line, err := readScalarLine(context.Background(), input)
	rest, readErr := io.ReadAll(input)
	if err != nil || readErr != nil || line != "apply" || string(rest) != "y\n" {
		t.Fatalf("read ahead: %q %q %v %v", line, rest, err, readErr)
	}
}
func TestApplyHighRiskConfigurationAndMultipleRemovals(t *testing.T) {
	for _, kind := range []string{"install", "configuration"} {
		t.Run(kind, func(t *testing.T) {
			b, e := applyFixture()
			b.preview.Operations[0].Kind = kind
			b.preview.Operations[0].Risk = "high"
			// Configuration of a dependency still requires specific review. Retain the
			// requested install for the coordinator's desired-present binding.
			if kind == "configuration" {
				op := b.preview.Operations[0]
				op.Package = "configured"
				op.OldVersion = "1"
				b.preview.Operations[0].Kind = "install"
				b.preview.Operations[0].Risk = "normal"
				b.preview.Operations = append(b.preview.Operations, op)
			}
			out, err := invokeApply(context.Background(), b, e, "apply\n", true)
			if !errors.Is(err, apply.ErrRejected) || e.calls != 0 || !strings.Contains(out, "review "+kind) {
				t.Fatalf("%v %s", err, out)
			}
		})
	}
	b, e := applyFixture()
	for _, name := range []string{"one", "two"} {
		b.preview.Operations = append(b.preview.Operations, apt.Operation{Kind: "removal", Package: name, Architecture: "amd64", OldVersion: "4", Installed: apt.PackageEvidence{Known: true}})
	}
	out, err := invokeApply(context.Background(), b, e, "remove one:amd64\nremove one:amd64\napply\n", true)
	if !errors.Is(err, apply.ErrRejected) || e.calls != 0 || !strings.Contains(out, "remove two:amd64") {
		t.Fatalf("%v %s", err, out)
	}
	for _, flag := range []string{"essential", "protected", "unknown"} {
		t.Run(flag, func(t *testing.T) {
			op := apt.Operation{Kind: "removal", Installed: apt.PackageEvidence{Known: true}}
			if flag == "essential" {
				op.Installed.Essential = true
			}
			if flag == "protected" {
				op.Installed.Protected = true
			}
			if flag == "unknown" {
				op.Installed.Known = false
			}
			var out bytes.Buffer
			r := &terminalReviewer{out: &out, capable: func() bool { t.Fatal("asked for bypass"); return true }}
			ok, err := r.ApproveRemoval(context.Background(), op)
			if ok || err == nil || out.Len() != 0 {
				t.Fatalf("%v %v %s", ok, err, out.String())
			}
		})
	}
}
func TestApplyPartialAndContextCancellationReports(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "partial"
		if cancelled {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			b, e := applyFixture()
			b.preview.Operations = append(b.preview.Operations, apt.Operation{Kind: "install", Package: "dep", Architecture: "amd64", NewVersion: "1"})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			e.run = func(context.Context) error {
				b.post = map[string]packages.Info{"git:amd64": {Name: "git:amd64", Status: packages.Current, InstalledVersion: "1"}}
				if cancelled {
					cancel()
					return context.Canceled
				}
				return errors.New("exit status 100: stopped")
			}
			out, err := invokeApply(ctx, b, e, "apply\n", true)
			expected := "partial"
			exit := 1
			if cancelled {
				expected = "cancellation"
				exit = 130
			}
			if err == nil || ExitCode(err) != exit || !strings.Contains(out, "Apply result: "+expected) || !strings.Contains(out, "verified=true") || !strings.Contains(out, "verified=false") {
				t.Fatalf("%v %s", err, out)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b, e := applyFixture()
	_, err := invokeApply(ctx, b, e, "apply\n", true)
	if !errors.Is(err, context.Canceled) || e.calls != 0 || ExitCode(err) != 130 {
		t.Fatal(err)
	}
}
func TestApplyResolutionEnvironmentAndBlockedDiagnostics(t *testing.T) {
	b, e := applyFixture()
	var out bytes.Buffer
	catalog := software.Catalog{"flat": {ID: "flat", Name: "Flat", Variants: []software.Variant{{Provider: software.Provider("flatpak"), Identifier: "flat"}}}}
	r := &terminalReviewer{out: &out}
	err := runApply(context.Background(), []string{"flat"}, &out, fakeSystem{}, b, catalog, e, r)
	if err == nil || e.calls != 0 || b.simulations != 0 || ExitCode(err) != 2 {
		t.Fatal("nonAPT accepted", err)
	}
	b, e = applyFixture()
	b.simulationErr = errors.New("unresolved simulation")
	b.preview.Simulation = apt.Diagnostic{Command: "fake simulation", Stderr: "solver diagnostic"}
	b.preview.Unresolved = true
	text, err := invokeApply(context.Background(), b, e, "apply\n", true)
	if err == nil || e.calls != 0 || strings.Contains(text, "Type ") || !strings.Contains(text, "solver diagnostic") {
		t.Fatalf("%v %s", err, text)
	}
	b, e = applyFixture()
	b.post = map[string]packages.Info{"git": {Name: "git", Status: packages.Unavailable}}
	text, err = invokeApply(context.Background(), b, e, "apply\n", true)
	if err == nil || e.calls != 0 || strings.Contains(text, "Type ") || !strings.Contains(text, "unavailable") {
		t.Fatalf("%v %s", err, text)
	}
}

func TestApplyCatalogOriginsAndResolvedOperandDedup(t *testing.T) {
	b, e := applyFixture()
	catalog := software.Catalog{
		"alias": {ID: "alias", Name: "Alias", Requires: []string{"dep"}, Variants: []software.Variant{{Provider: software.APT, Identifier: "git"}}},
		"dep":   {ID: "dep", Name: "Dependency", Variants: []software.Variant{{Provider: software.APT, Identifier: "git"}}},
	}
	var out bytes.Buffer
	r := &terminalReviewer{out: &out, capable: func() bool { return true }, readLine: func(context.Context) (string, error) { return "apply", nil }}
	err := runApply(context.Background(), []string{"alias", "alias"}, &out, fakeSystem{}, b, catalog, e, r)
	if err != nil || e.calls != 1 || strings.Join(b.operands, ",") != "git" {
		t.Fatalf("%v calls=%d operands=%v", err, e.calls, b.operands)
	}
	for _, text := range []string{"requested software alias", "catalog dependency dep", "Requested package state: git status=available installed=unknown candidate=1", "Provider: apt"} {
		if !strings.Contains(out.String(), text) {
			t.Fatalf("missing %q: %s", text, out.String())
		}
	}
}

func TestInteractiveNoTerminalWithFakeExecution(t *testing.T) {
	b, e := applyFixture()
	var out bytes.Buffer
	err := RunInteractive(context.Background(), []string{"apply", "git"}, strings.NewReader("apply\n"), &out, &out, fakeSystem{}, b, e)
	if err == nil || e.calls != 0 || strings.Contains(out.String(), "Type ") {
		t.Fatalf("%v %s", err, out.String())
	}
}
