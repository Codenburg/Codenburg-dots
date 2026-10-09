package apt

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

const simCommand = "apt-get --simulate -o APT::Get::Simulate=true -o Debug::NoLocking=true -o APT::Get::AutomaticRemove=false -o APT::Get::Show-User-Simulation-Note=false install -- app old"

func record(name, version, flags string) string {
	return "Package: " + name + "\nArchitecture: amd64\nVersion: " + version + "\n" + flags + "\n"
}
func simulationFixture(out string) *fakeRunner {
	return &fakeRunner{results: map[string]result{
		"dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- app": {stderr: "dpkg-query: no packages found matching app", err: commandError(1)},
		"apt-cache policy app": {out: "Candidate: 2\n"},
		"dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- old": {out: "ii \t1\n"},
		"apt-cache policy old":             {out: "Candidate: 2\n"},
		"dpkg --compare-versions 1 ge 2":   {err: commandError(1)},
		"dpkg --print-architecture":        {out: "amd64\n"},
		simCommand:                         {out: out},
		"apt-cache show -- app:amd64=2":    {out: record("app", "2", "Essential: no\nProtected: no")},
		"apt-cache show -- dep:amd64=2":    {out: record("dep", "2", "Essential: no\nProtected: no")},
		"apt-cache show -- old:amd64=2":    {out: record("old", "2", "Essential: no\nProtected: no")},
		"dpkg-query --status -- old:amd64": {out: record("old", "1", "Status: install ok installed\nEssential: no\nProtected: no\nConffiles:")},
		"dpkg-query --status -- gone":      {out: record("gone", "1", "Status: install ok installed\nEssential: no\nProtected: no\nConffiles:")},
	}}
}

const combined = "Reading package lists... Done\nBuilding dependency tree... Done\nReading state information... Done\n1 upgraded, 2 newly installed, 1 to remove and 0 not upgraded.\nRemv gone [1]\nInst app (2 Repo [amd64])\nInst dep (2 Repo [amd64])\nInst old [1] (2 Repo [amd64])\nConf app (2 Repo [amd64])\nConf dep (2 Repo [amd64])\nConf old (2 Repo [amd64])\n"

func TestSimulateWholeTransaction(t *testing.T) {
	f := simulationFixture(combined)
	got, err := New(f).Simulate(context.Background(), []string{"old", "app", "old"})
	if err != nil || got.Unresolved || len(got.Operations) != 7 {
		t.Fatalf("preview=%+v error=%v", got, err)
	}
	if !reflect.DeepEqual(got.Requests, []string{"app", "old"}) {
		t.Fatalf("requests: %v", got.Requests)
	}
	counts := map[string]int{}
	for _, op := range got.Operations {
		counts[op.Kind]++
		if op.Package == "dep" && (op.Requested || op.Risk != "informational") {
			t.Fatalf("dependency: %+v", op)
		}
	}
	if counts["install"] != 2 || counts["upgrade"] != 1 || counts["removal"] != 1 || counts["configuration"] != 3 {
		t.Fatal(counts)
	}
	if len(got.ReviewRequirements) == 0 {
		t.Fatal("missing removal/upgrade review")
	}
	n := 0
	for _, call := range f.calls {
		if strings.HasPrefix(call, "apt-get ") {
			n++
			if call != simCommand {
				t.Fatal(call)
			}
		}
	}
	if n != 1 {
		t.Fatalf("simulations=%d calls=%v", n, f.calls)
	}
	if got.Simulation.Stdout != combined {
		t.Fatal("lost diagnostics")
	}
}

func TestSimulateRejectsOperandsBeforeCommands(t *testing.T) {
	for _, name := range []string{"-y", "app;true", "app=2", "app+", "app-", "app/amd64", "app:unknown"} {
		t.Run(name, func(t *testing.T) {
			f := simulationFixture(combined)
			got, err := New(f).Simulate(context.Background(), []string{name})
			if err == nil || !got.Unresolved {
				t.Fatal("accepted", name)
			}
			if name != "app:unknown" && len(f.calls) != 0 {
				t.Fatal(f.calls)
			}
		})
	}
}

func TestSimulateIntegrityAndDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, out   string
		diagnostics []string
		operations  int
	}{
		{name: "missing summary", out: "Inst app (2 Repo [amd64])\n", diagnostics: []string{"missing transaction summary", "unpack lacks completion/configuration evidence"}, operations: 1},
		{name: "malformed summary", out: strings.Replace(combined, "2 newly", "two newly", 1), diagnostics: []string{"unrecognized simulation line", "missing transaction summary"}, operations: 7},
		{name: "truncated", out: strings.Split(combined, "Conf app")[0], diagnostics: []string{"unpack lacks completion/configuration evidence", "transaction counts contradict operation records"}, operations: 4},
		{name: "bad count", out: strings.Replace(combined, "2 newly", "3 newly", 1), diagnostics: []string{"transaction counts contradict operation records"}, operations: 7},
		{name: "unknown syntax", out: combined + "Mystery app\n", diagnostics: []string{"unrecognized simulation line \"Mystery app\""}, operations: 7},
		{name: "broken bracket", out: combined + "Inst extra (2 Repo [amd64]) [bad]\n", diagnostics: []string{"unrecognized simulation line \"Inst extra"}, operations: 7},
		{name: "invalid version", out: strings.Replace(combined, "Inst app (2", "Inst app (invalid", 1), diagnostics: []string{"invalid reported version", "configuration contradicts unpack version/order"}, operations: 7},
		{name: "contradictory version", out: strings.Replace(combined, "Conf app (2", "Conf app (3", 1), diagnostics: []string{"configuration contradicts unpack version/order", "operation contradicts inspected candidate version"}, operations: 7},
		{name: "foreign architecture", out: strings.ReplaceAll(combined, "[amd64]", "[arm64]"), diagnostics: []string{"unsupported operation architecture \"arm64\""}, operations: 7},
		{name: "contradictory removal", out: combined + "Remv app [2]\n", diagnostics: []string{"operation contradicts requested installed state", "contradictory operations for app"}, operations: 8},
		{name: "duplicate", out: combined + "Conf app (2 Repo [amd64])\n", diagnostics: []string{"duplicate operation app:amd64/configuration"}, operations: 8},
		{name: "ambiguous install version", out: combined + "Inst app (3 Repo [amd64])\n", diagnostics: []string{"duplicate operation app:amd64/install", "operation contradicts inspected candidate version"}, operations: 8},
		{name: "unchanged upgrade", out: strings.Replace(combined, "Inst old [1]", "Inst old [2]", 1), diagnostics: []string{"upgrade reports unchanged version", "operation contradicts inspected old version"}, operations: 7},
		{name: "ambiguous multiarch", out: strings.Replace(combined, "Conf old (2 Repo [amd64])", "Conf old:arm64 (2 Repo [arm64])", 1), diagnostics: []string{"ambiguous multiarch operation old", "unpack lacks completion/configuration evidence"}, operations: 7},
		{name: "unsupported heading", out: combined + "The following packages will be PURGED:\n  old\n", diagnostics: []string{"unrecognized simulation line \"The following packages will be PURGED:"}, operations: 7},
		{name: "contradictory no-op", out: combined + "old is already the newest version (9).\n", diagnostics: []string{"newest-version evidence contradicts inspection for old"}, operations: 7},
		{name: "configuration before unpack", out: "Conf app (2 Repo [amd64])\n" + strings.Replace(combined, "Conf app (2 Repo [amd64])\n", "", 1), diagnostics: []string{"configuration contradicts unpack version/order"}, operations: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New(simulationFixture(tc.out)).Simulate(context.Background(), []string{"app", "old"})
			if err == nil || !got.Unresolved {
				t.Fatalf("accepted %+v", got)
			}
			for _, diagnostic := range tc.diagnostics {
				if !strings.Contains(err.Error(), diagnostic) {
					t.Errorf("error=%v, want diagnostic %q", err, diagnostic)
				}
			}
			if got.Simulation.Stderr != "" || got.Simulation.Err != nil || got.Simulation.Stdout != tc.out {
				t.Fatalf("integrity rejection masked by command failure or lost diagnostics: %+v", got.Simulation)
			}
			// Retain every parsed operation plus the explicit unresolved marker.
			if len(got.Operations) != tc.operations+1 || got.Operations[tc.operations].Kind != "unresolved" {
				t.Fatalf("lost partial evidence: %+v", got.Operations)
			}
			for _, op := range got.Operations[:tc.operations] {
				if op.Kind == "unresolved" || op.Package == "" {
					t.Fatalf("replaced reported operation: %+v", op)
				}
			}
		})
	}
}

func TestSimulateCommandFailureRetainsDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, out, stderr, diagnostic string
		err                           error
	}{
		{name: "nonzero", out: combined, stderr: "diagnostic", err: commandError(100)},
		{name: "deadline", out: combined, stderr: "diagnostic", err: context.DeadlineExceeded},
		{name: "output bound", out: combined + strings.Repeat("x", maxCommandOutput+1), diagnostic: "stdout limit exceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := simulationFixture(tc.out)
			f.results[simCommand] = result{out: tc.out, stderr: tc.stderr, err: tc.err}
			got, err := New(f).Simulate(context.Background(), []string{"app", "old"})
			if err == nil || !got.Unresolved || got.Simulation.Err == nil {
				t.Fatalf("accepted %+v error=%v", got, err)
			}
			if tc.err != nil && (!errors.Is(err, tc.err) || !errors.Is(got.Simulation.Err, tc.err)) {
				t.Fatal(err)
			}
			if tc.diagnostic != "" && (!strings.Contains(err.Error(), tc.diagnostic) || !strings.Contains(got.Simulation.Err.Error(), tc.diagnostic)) {
				t.Fatalf("error=%v, command error=%v, want %q", err, got.Simulation.Err, tc.diagnostic)
			}
			wantOut := tc.out
			if len(wantOut) > maxCommandOutput {
				wantOut = wantOut[:maxCommandOutput]
			}
			if got.Simulation.Stdout != wantOut || got.Simulation.Stderr != tc.stderr || len(got.Operations) != 8 || got.Operations[7].Kind != "unresolved" {
				t.Fatalf("lost bounded diagnostics/operations: %+v", got)
			}
		})
	}
}

func TestSimulateMetadataRisk(t *testing.T) {
	key := "dpkg-query --status -- gone"
	for _, tc := range []struct {
		name, flags string
		unresolved  bool
		risk        string
	}{
		{name: "essential", flags: "Status: install ok installed\nEssential: yes\nProtected: no", risk: "high"},
		{name: "protected", flags: "Status: install ok installed\nEssential: no\nProtected: yes", risk: "high"},
		{name: "configuration sensitive", flags: "Status: install ok installed\nEssential: no\nProtected: no\nConffiles: /etc/example hash", risk: "review-required"},
		{name: "unknown", flags: "Status: install ok installed\nEssential: no\nProtected:", unresolved: true, risk: "review-required"},
		{name: "high despite unknown", flags: "Status: install ok installed\nEssential: yes\nProtected: invalid", unresolved: true, risk: "high"},
		{name: "partial state", flags: "Status: install ok unpacked\nEssential: no\nProtected: no", unresolved: true, risk: "review-required"},
		{name: "conflicting flag", flags: "Status: install ok installed\nEssential: no\nEssential: yes\nProtected: no", unresolved: true, risk: "high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := combined
			if tc.name == "essential" {
				out = "WARNING: The following essential packages will be removed.\n" +
					"This should NOT be done unless you know exactly what you are doing!\n  gone\n" + combined
			}
			f := simulationFixture(out)
			f.results[key] = result{out: record("gone", "1", tc.flags)}
			got, err := New(f).Simulate(context.Background(), []string{"app", "old"})
			if (err != nil) != tc.unresolved || got.Unresolved != tc.unresolved {
				t.Fatalf("%+v %v", got, err)
			}
			for _, op := range got.Operations {
				if op.Package == "gone" && op.Risk != tc.risk {
					t.Fatalf("risk %+v", op)
				}
			}
		})
	}
}

func TestSimulateAmbiguousMetadata(t *testing.T) {
	for _, out := range []string{
		record("app", "3", "Essential: no\nProtected: no"),
		record("other", "2", "Essential: no\nProtected: no"),
		strings.Replace(record("app", "2", "Essential: no\nProtected: no"), "amd64", "arm64", 1),
		record("app", "2", "Essential: no\nProtected: no") + "\n" + record("app", "2", "Essential: yes\nProtected: no"),
	} {
		f := simulationFixture(combined)
		f.results["apt-cache show -- app:amd64=2"] = result{out: out}
		got, err := New(f).Simulate(context.Background(), []string{"app", "old"})
		if err == nil || !got.Unresolved {
			t.Fatalf("accepted metadata %q", out)
		}
	}
}

func TestSimulateAlternateTransactions(t *testing.T) {
	for _, tc := range []struct {
		name, request, out string
		review             bool
	}{
		{name: "direct install", request: "app", out: "0 upgraded, 1 newly installed, 0 to remove and 0 not upgraded.\nInst app (2 Repo [amd64])\nConf app (2 Repo [amd64])\n"},
		{name: "upgrade only", request: "old", out: "1 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\nInst old [1] (2 Repo [amd64])\nConf old (2 Repo [amd64])\n", review: true},
		{name: "removal only", request: "old", out: "0 upgraded, 0 newly installed, 1 to remove and 0 not upgraded.\nRemv gone [1]\n", review: true},
		{name: "no-op", request: "old", out: "old is already the newest version (1).\n0 upgraded, 0 newly installed, 0 to remove and 1 not upgraded.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := simulationFixture(tc.out)
			key := strings.TrimSuffix(simCommand, "app old") + tc.request
			f.results[key] = result{out: tc.out}
			got, err := New(f).Simulate(context.Background(), []string{tc.request})
			if err != nil || got.Unresolved || (len(got.ReviewRequirements) > 0) != tc.review {
				t.Fatalf("preview=%+v err=%v", got, err)
			}
			if tc.name == "direct install" && got.Operations[0].Risk != "normal" {
				t.Fatal(got.Operations)
			}
			if tc.name == "no-op" && len(got.Operations) != 0 {
				t.Fatal(got.Operations)
			}
		})
	}
}

func TestSimulateInspectionAmbiguityAndFailureRetainsDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, key string
		r         result
	}{
		{name: "ambiguous candidate", key: "apt-cache policy app", r: result{out: "Candidate: 2\nCandidate: 3\n"}},
		{name: "invalid version", key: "apt-cache policy app", r: result{out: "Candidate: --option\n"}},
		{name: "failed inspector", key: "apt-cache policy app", r: result{out: "partial policy", stderr: "cache error", err: commandError(100)}},
		{name: "bounded inspector", key: "apt-cache policy app", r: result{out: strings.Repeat("x", maxCommandOutput+1)}},
		{name: "ambiguous installed", key: "dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- old", r: result{out: "ii \t1\nii \t2\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := simulationFixture(combined)
			f.results[tc.key] = tc.r
			got, err := New(f).Simulate(context.Background(), []string{"app", "old"})
			if err == nil || !got.Unresolved {
				t.Fatalf("accepted %+v", got)
			}
			found := false
			for _, d := range got.Metadata {
				if d.Command == tc.key {
					found = true
					if d.Stderr != tc.r.stderr || len(d.Stdout) > maxCommandOutput {
						t.Fatal("lost diagnostics")
					}
				}
			}
			if !found {
				t.Fatal("missing inspection diagnostic")
			}
			for _, c := range f.calls {
				if strings.HasPrefix(c, "apt-get ") {
					t.Fatal("continued after failed inspection")
				}
			}
		})
	}
}

func TestSimulateNewCriticalAndUnknownMetadata(t *testing.T) {
	for _, flags := range []string{"Essential: yes\nProtected: no", "Essential: no\nProtected:"} {
		f := simulationFixture(combined)
		f.results["apt-cache show -- app:amd64=2"] = result{out: record("app", "2", flags)}
		got, err := New(f).Simulate(context.Background(), []string{"app", "old"})
		if strings.Contains(flags, "yes") {
			if err != nil || got.Operations[1].Risk != "high" {
				t.Fatalf("%+v %v", got, err)
			}
		} else if err == nil || len(got.Operations[1].Uncertainty) == 0 {
			t.Fatal("missing uncertainty")
		}
	}
}

func TestSimulateStandaloneConfiguration(t *testing.T) {
	out := "0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n1 not fully installed or removed.\nConf old (2 Repo [amd64])\n"
	f := simulationFixture(out)
	f.results[strings.TrimSuffix(simCommand, "app old")+"old"] = result{out: out}
	f.results["dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- old"] = result{out: "ii \t2\n"}
	f.results["apt-cache policy old"] = result{out: "Candidate: 3\n"}
	f.results["dpkg --compare-versions 2 ge 3"] = result{err: commandError(1)}
	f.results["dpkg-query --status -- old:amd64"] = result{out: record("old", "2", "Status: install ok installed\nEssential: no\nProtected: no\nConffiles: /etc/example hash")}
	got, err := New(f).Simulate(context.Background(), []string{"old"})
	if err != nil || len(got.Operations) != 1 || got.Operations[0].Kind != "configuration" || got.Operations[0].Risk != "review-required" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSimulateRemovedMultiarchAmbiguity(t *testing.T) {
	f := simulationFixture(combined)
	f.results["dpkg-query --status -- gone"] = result{out: record("gone", "1", "Status: install ok installed\nEssential: no\nProtected: no") + "\n" + strings.Replace(record("gone", "1", "Status: install ok installed\nEssential: yes\nProtected: no"), "amd64", "arm64", 1)}
	got, err := New(f).Simulate(context.Background(), []string{"app", "old"})
	if err == nil || !got.Unresolved || len(got.Operations) == 0 || got.Operations[0].Architecture != "" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSimulateCanceledParent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := New(simulationFixture(combined)).Simulate(ctx, []string{"app", "old"})
	if !errors.Is(err, context.Canceled) || !got.Unresolved {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSimulateRejectsEmptyOrAmbiguousRequests(t *testing.T) {
	for _, names := range [][]string{nil, {"app", "app:amd64"}} {
		f := simulationFixture(combined)
		got, err := New(f).Simulate(context.Background(), names)
		if err == nil || !got.Unresolved || len(f.calls) != 0 {
			t.Fatalf("%+v %v %v", got, err, f.calls)
		}
	}
}

type deadlineRunner struct {
	*fakeRunner
	t *testing.T
}

func (f deadlineRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	d, ok := ctx.Deadline()
	if !ok || time.Until(d) > simulationTimeout {
		f.t.Fatal("unbounded deadline")
	}
	return f.fakeRunner.Run(ctx, name, args...)
}
func TestSimulateDeadlineDeterminismAndCache(t *testing.T) {
	f := simulationFixture(combined)
	a, err := New(deadlineRunner{fakeRunner: f, t: t}).Simulate(context.Background(), []string{"old", "app"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(simulationFixture(combined)).Simulate(context.Background(), []string{"app", "old"})
	if err != nil || !reflect.DeepEqual(a, b) {
		t.Fatalf("nondeterministic: %v", err)
	}
	calls := map[string]int{}
	for _, c := range f.calls {
		calls[c]++
		if calls[c] > 1 {
			t.Fatalf("repeated inspection/metadata %s", c)
		}
	}
}
