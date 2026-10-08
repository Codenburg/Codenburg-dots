package cli

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/Codenburg/Codenburg-dots/internal/providers/apt"
	"github.com/Codenburg/Codenburg-dots/internal/software"
)

type preflightSimulator struct {
	preview     apt.Preview
	err         error
	calls       [][]string
	inspections int
}

func (s *preflightSimulator) Inspect(context.Context, string) (PackageInfo, error) {
	s.inspections++
	return PackageInfo{}, errors.New("CLI must not duplicate provider inspection")
}

func (s *preflightSimulator) Simulate(_ context.Context, names []string) (apt.Preview, error) {
	s.calls = append(s.calls, append([]string{}, names...))
	return s.preview, s.err
}

func completedPreview(ops ...apt.Operation) apt.Preview {
	return apt.Preview{
		Requests: []string{"bash", "git", "neovim"}, Operations: ops,
		Simulation: apt.Diagnostic{Command: "apt-get --simulate install -- bash git neovim"},
		Warnings:   []string{"simulation is a non-atomic snapshot"},
	}
}

func TestPreflightWholeTransactionAndProvenance(t *testing.T) {
	s := &preflightSimulator{preview: completedPreview(
		apt.Operation{Kind: "install", Package: "git", Architecture: "amd64", NewVersion: "2", Requested: true, Risk: "normal"},
		apt.Operation{Kind: "install", Package: "libgit", Architecture: "amd64", NewVersion: "3", Risk: "informational"},
		apt.Operation{Kind: "configuration", Package: "libgit", Architecture: "amd64", NewVersion: "3", Risk: "informational"},
	)}
	info, _ := (fakeSystem{}).Detect()
	d := &planDetector{info: info}
	var out, stderr bytes.Buffer
	err := Run([]string{"preflight", "neovim", "git", "bash", "git"}, &out, &stderr, d, s)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.calls, [][]string{{"bash", "git", "neovim"}}) || s.inspections != 0 || d.calls != 1 {
		t.Fatalf("not one bounded provider transaction: calls %v inspections %d detections %d", s.calls, s.inspections, d.calls)
	}
	for _, want := range []string{
		"Requested software: bash, git, neovim", "Provider: apt", "Resolved package: git", "requested software git",
		"Operation: installation", "Operation: dependency installation", "Package: libgit", "Source: additional APT-selected package",
		"Operation: configuration", "New version: 3", "Architecture: amd64", "Risk: informational",
		"Simulation result: succeeded", "Overall status: preview-complete", "No changes applied.", "Execution is not implemented",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr %q", stderr.String())
	}
}

func TestPreflightCatalogDependenciesAreNotDirectRequests(t *testing.T) {
	catalog := software.Catalog{
		"editor": {ID: "editor", Name: "Editor", Requires: []string{"core"}, Variants: []software.Variant{{Provider: software.APT, Identifier: "neovim"}}},
		"core":   {ID: "core", Name: "Core", Variants: []software.Variant{{Provider: software.APT, Identifier: "bash"}}},
		"alias":  {ID: "alias", Name: "Alias", Variants: []software.Variant{{Provider: software.APT, Identifier: "neovim"}}},
	}
	s := &preflightSimulator{preview: completedPreview(
		apt.Operation{Kind: "install", Package: "bash", NewVersion: "1", Requested: true, Risk: "normal"},
		apt.Operation{Kind: "install", Package: "extra", NewVersion: "2", Risk: "informational"},
	)}
	s.preview.Requests = []string{"bash", "neovim"}
	s.preview.Simulation.Command = "apt-get --simulate install -- bash neovim"
	var out bytes.Buffer
	if err := runPreflight([]string{"editor", "alias"}, &out, fakeSystem{}, s, catalog); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.calls, [][]string{{"bash", "neovim"}}) || s.inspections != 0 {
		t.Fatalf("duplicated operands/inspection: %+v", s)
	}
	for _, want := range []string{"Requested software: alias, editor", "Source: catalog dependency core (Core)", "requested software alias (Alias); requested software editor (Editor)", "Operation: dependency installation\nPackage: bash", "Source: additional APT-selected package"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "requested software core") {
		t.Fatal("APT Requested flag confused with direct software request")
	}
}

func TestPreflightReviewAndCombinedVersions(t *testing.T) {
	for _, tc := range []struct{ name, risk, status string }{
		{name: "review removal", risk: "review-required", status: "review-required"},
		{name: "essential removal", risk: "high", status: "high-risk"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := completedPreview(
				apt.Operation{Kind: "upgrade", Package: "bash", Architecture: "amd64", OldVersion: "1", NewVersion: "2", Requested: true, Risk: "review-required"},
				apt.Operation{Kind: "removal", Package: "old", Architecture: "amd64", OldVersion: "4", Risk: tc.risk},
				apt.Operation{Kind: "configuration", Package: "standalone", NewVersion: "5", Risk: "review-required"},
				apt.Operation{Kind: "install", Package: "git", NewVersion: "6", Requested: true, Risk: "normal"},
			)
			v.ReviewRequirements = []string{"old: removal (" + tc.risk + ")"}
			if tc.risk == "high" {
				v.Warnings = append(v.Warnings, "WARNING: The following essential packages will be removed. old")
			}
			s := &preflightSimulator{preview: v}
			var out, stderr bytes.Buffer
			if err := Run([]string{"preflight", "bash", "git", "neovim"}, &out, &stderr, fakeSystem{}, s); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Operation: upgrade", "Old version: 1\nNew version: 2", "Operation: removal", "Old version: 4", "Package: standalone", "Architecture: unknown", "Overall status: " + tc.status, "Review requirement: old: removal", "old: operation-specific removal approval required", "No changes applied."} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("missing %q in:\n%s", want, out.String())
				}
			}
			if tc.risk == "high" && (!strings.Contains(out.String(), "additional safeguards required") || !strings.Contains(out.String(), "essential packages")) {
				t.Fatal("high-risk safeguards/evidence hidden")
			}
		})
	}
}

func TestPreflightUnresolvedRetainsDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name          string
		simulationErr error
		failure       error
	}{
		{name: "incomplete safety metadata", failure: errors.New("Protected evidence unavailable")},
		{name: "malformed output", failure: errors.New("malformed simulation record")},
		{name: "partial command failure", simulationErr: errors.New("apt-get failed"), failure: errors.New("apt-get failed")},
		{name: "deadline", simulationErr: context.DeadlineExceeded, failure: context.DeadlineExceeded},
		{name: "ambiguous versions", failure: errors.New("ambiguous candidate version")},
		{name: "missing executable", simulationErr: errors.New("apt-get: executable file not found"), failure: errors.New("apt-get: executable file not found")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := completedPreview(
				apt.Operation{Kind: "install", Package: "git", NewVersion: "2", Requested: true, Risk: "unresolved", Uncertainty: []string{tc.failure.Error()}},
				apt.Operation{Kind: "configuration", Package: "git", NewVersion: "2", Requested: true, Risk: "unresolved"},
				apt.Operation{Kind: "unresolved", Risk: "unresolved", Uncertainty: []string{tc.failure.Error()}},
			)
			v.Unresolved = true
			v.Simulation.Err = tc.simulationErr
			v.Simulation.Stdout = "retained partial simulation output\n"
			v.Simulation.Stderr = "retained simulation stderr\n"
			v.Metadata = []apt.Diagnostic{{Command: "apt-cache show git=2", Stdout: "retained metadata output", Stderr: "metadata warning", Err: tc.failure}}
			s := &preflightSimulator{preview: v, err: tc.failure}
			var out, stderr bytes.Buffer
			err := Run([]string{"preflight", "git"}, &out, &stderr, fakeSystem{}, s)
			if !errors.Is(err, tc.failure) {
				t.Fatalf("failure not propagated: %v", err)
			}
			for _, want := range []string{"Operation: installation", "Operation: configuration", "Operation: unresolved", "Overall status: unresolved", "Uncertainty: " + tc.failure.Error(), "retained metadata output", "metadata warning", "No changes applied.", "not safely actionable"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("missing %q in:\n%s", want, out.String())
				}
			}
			if tc.simulationErr != nil {
				for _, want := range []string{"Simulation result: failed", "retained partial simulation output", "retained simulation stderr"} {
					if !strings.Contains(out.String(), want) {
						t.Errorf("missing %q", want)
					}
				}
			} else if !strings.Contains(out.String(), "Simulation result: succeeded") {
				t.Fatal("metadata uncertainty hid successful simulation command")
			}
		})
	}
}

func TestPreflightValidationAndEnvironment(t *testing.T) {
	info, _ := (fakeSystem{}).Detect()
	unsupported := info
	unsupported.ID = "arch"
	unsupported.Supported = false
	noAPT := info
	noAPT.APTAvailable = false
	badArch := info
	badArch.RawArchitecture = "riscv64"
	badArch.Architecture = "riscv64"
	for _, tc := range []struct {
		name        string
		args        []string
		info        SystemInfo
		noSimulator bool
		want        string
		detections  int
	}{
		{name: "missing args", args: []string{"preflight"}, want: "usage: cdots preflight <software-id>..."},
		{name: "unknown before host", args: []string{"preflight", "missing"}, info: unsupported, want: "unknown software ID"},
		{name: "invalid ID", args: []string{"preflight", "--bad"}, info: info, want: "invalid software ID"},
		{name: "unsupported distribution", args: []string{"preflight", "bash"}, info: unsupported, want: "unsupported distribution", detections: 1},
		{name: "APT unavailable", args: []string{"preflight", "bash"}, info: noAPT, want: "APT inspection requires", detections: 1},
		{name: "unsupported raw architecture", args: []string{"preflight", "bash"}, info: badArch, want: "unsupported architecture", detections: 1},
		{name: "inspector without simulator", args: []string{"preflight", "bash"}, info: info, noSimulator: true, want: "APT transaction simulation is unavailable", detections: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &planDetector{info: tc.info}
			s := &preflightSimulator{}
			var inspector PackageInspector = s
			if tc.noSimulator {
				inspector = fakePackages{}
			}
			var out, stderr bytes.Buffer
			err := Run(tc.args, &out, &stderr, d, inspector)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			if len(s.calls) != 0 || s.inspections != 0 || d.calls != tc.detections {
				t.Fatalf("unexpected work: %+v, detections %d", s, d.calls)
			}
		})
	}
}

type preflightRunner func(context.Context, string, ...string) (string, string, error)

func (r preflightRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	return r(ctx, name, args...)
}

// Exercise the actual provider boundary, not only a canned simulator result.
func TestPreflightProviderReadOnlyAudit(t *testing.T) {
	const command = "apt-get --simulate -o APT::Get::Simulate=true -o Debug::NoLocking=true -o APT::Get::AutomaticRemove=false -o APT::Get::Show-User-Simulation-Note=false install -- bash git neovim"
	const output = "0 upgraded, 3 newly installed, 1 to remove and 0 not upgraded.\nRemv gone [1]\nInst git (2 Repo [amd64])\nInst neovim (2 Repo [amd64])\nInst lib (2 Repo [amd64])\nConf git (2 Repo [amd64])\nConf neovim (2 Repo [amd64])\nConf lib (2 Repo [amd64])\n"
	const metadataFormat = "Package: ${Package}\\nArchitecture: ${Architecture}\\nVersion: ${Version}\\nStatus: ${Status}\\nEssential: ${Essential}\\nProtected: ${Protected}\\nConffiles: ${Conffiles}\\n\\n"
	for _, tc := range []struct {
		name       string
		commandErr error
	}{
		{name: "complete read-only preview"},
		{name: "missing apt-get", commandErr: exec.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			counts := make(map[string]int)
			calls := []string{}
			runner := preflightRunner(func(ctx context.Context, name string, args ...string) (string, string, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("unbounded provider command")
				}
				key := name + " " + strings.Join(args, " ")
				counts[key]++
				calls = append(calls, key)
				switch key {
				case "dpkg --print-architecture":
					return "amd64\n", "", nil
				case "dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- bash":
					return "ii \t1\n", "", nil
				case "dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- git", "dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- neovim":
					return "rc \t\n", "", nil
				case "apt-cache policy bash":
					return "Candidate: 1\n", "", nil
				case "apt-cache policy git", "apt-cache policy neovim":
					return "Candidate: 2\n", "", nil
				case "dpkg --compare-versions 1 ge 1":
					return "", "", nil
				case command:
					if tc.commandErr != nil {
						return "retained failed stdout", "apt-get executable unavailable", tc.commandErr
					}
					return output, "", nil
				case "apt-cache show -- git:amd64=2", "apt-cache show -- neovim:amd64=2", "apt-cache show -- lib:amd64=2":
					pkg, _, _ := strings.Cut(strings.TrimSuffix(args[2], "=2"), ":")
					return "Package: " + pkg + "\nArchitecture: amd64\nVersion: 2\nEssential: no\nProtected: no\n", "", nil
				case "dpkg-query -W -f=" + metadataFormat + " -- gone":
					return "Package: gone\nArchitecture: amd64\nVersion: 1\nStatus: install ok installed\nEssential: no\nProtected: no\nConffiles:\n", "", nil
				default:
					t.Fatalf("unexpected or mutating command: %s", key)
					return "", "", errors.New("unexpected command")
				}
			})
			var out, stderr bytes.Buffer
			err := Run([]string{"preflight", "neovim", "bash", "git", "git"}, &out, &stderr, fakeSystem{}, apt.New(runner))
			if tc.commandErr == nil {
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{"Operations retained: 7", "Overall status: review-required", "Package: lib", "No changes applied."} {
					if !strings.Contains(out.String(), want) {
						t.Errorf("missing %q in %s", want, out.String())
					}
				}
			} else if !errors.Is(err, tc.commandErr) || !strings.Contains(out.String(), "apt-get executable unavailable") || !strings.Contains(out.String(), "retained failed stdout") {
				t.Fatalf("missing executable/diagnostics hidden: %v\n%s", err, out.String())
			}
			if counts[command] != 1 {
				t.Fatalf("not one complete simulation: %v", calls)
			}
			for _, pkg := range []string{"bash", "git", "neovim"} {
				if counts["dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- "+pkg] != 1 {
					t.Fatalf("duplicated requested inspection: %v", calls)
				}
			}
		})
	}
}

func TestPreflightNoOpAndSupportedArchitecture(t *testing.T) {
	for _, raw := range []string{"x86_64", "aarch64"} {
		t.Run(raw, func(t *testing.T) {
			info, _ := (fakeSystem{}).Detect()
			info.RawArchitecture = raw
			s := &preflightSimulator{preview: completedPreview()}
			var out, stderr bytes.Buffer
			if err := Run([]string{"preflight", "bash"}, &out, &stderr, &planDetector{info: info}, s); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "Operations retained: 0") || !strings.Contains(out.String(), "Overall status: preview-complete") {
				t.Fatal(out.String())
			}
		})
	}
}

type preflightFailingWriter struct{ err error }

func (w preflightFailingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestPreflightOutputAndUnresolvedFlagErrors(t *testing.T) {
	failure := errors.New("output unavailable")
	s := &preflightSimulator{preview: completedPreview()}
	s.preview.Unresolved = true
	var stderr bytes.Buffer
	err := Run([]string{"preflight", "bash"}, preflightFailingWriter{err: failure}, &stderr, fakeSystem{}, s)
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "evidence is unresolved") {
		t.Fatalf("joined errors missing: %v", err)
	}
}

func TestPreflightDeterministicWithoutChangingEvidence(t *testing.T) {
	ops := []apt.Operation{
		{Kind: "configuration", Package: "git", NewVersion: "2", Requested: true, Risk: "normal"},
		{Kind: "install", Package: "git", NewVersion: "2", Requested: true, Risk: "normal"},
		{Kind: "install", Package: "aaa", NewVersion: "3", Risk: "informational"},
	}
	var expected string
	for i := 0; i < 4; i++ {
		ordered := append([]apt.Operation{}, ops...)
		if i%2 != 0 {
			ordered[0], ordered[2] = ordered[2], ordered[0]
		}
		s := &preflightSimulator{preview: completedPreview(ordered...)}
		before := append([]apt.Operation{}, s.preview.Operations...)
		args := []string{"preflight", "git", "bash", "neovim"}
		if i%2 != 0 {
			args[1], args[3] = args[3], args[1]
		}
		var out, stderr bytes.Buffer
		if err := Run(args, &out, &stderr, fakeSystem{}, s); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			expected = out.String()
		} else if expected != out.String() {
			t.Fatalf("nondeterministic display:\n%s", out.String())
		}
		if !reflect.DeepEqual(before, s.preview.Operations) {
			t.Fatal("rendering mutated provider evidence")
		}
	}
}
