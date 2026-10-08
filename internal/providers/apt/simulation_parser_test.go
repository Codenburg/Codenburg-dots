package apt

import (
	"context"
	"strings"
	"testing"
)

func TestSimulationProgressAndLists(t *testing.T) {
	for _, tc := range []struct {
		name, prefix string
		bad          bool
	}{
		{name: "progress without Done", prefix: "Reading package lists...\nBuilding dependency tree...\nReading state information...\n"},
		{name: "valid operation lists", prefix: "The following NEW packages will be installed:\n  app dep\nThe following packages will be upgraded:\n  old\nThe following packages will be REMOVED:\n  gone\n"},
		{name: "disguised operation", prefix: "The following NEW packages will be installed:\n  Inst app (2 Repo [amd64])\n", bad: true},
		{name: "truncated operation", prefix: "The following NEW packages will be installed:\n  Inst app\n", bad: true},
		{name: "empty list", prefix: "The following NEW packages will be installed:\n", bad: true},
		{name: "contradictory list", prefix: "The following NEW packages will be installed:\n  old\n", bad: true},
		{name: "unreported package", prefix: "The following NEW packages will be installed:\n  missing\n", bad: true},
		{name: "incomplete list", prefix: "The following NEW packages will be installed:\n  app\n", bad: true},
		{name: "duplicate list", prefix: "The following NEW packages will be installed:\n  app app\n", bad: true},
		{name: "held disguised operation", prefix: "The following held packages will be changed:\n  Inst app\n", bad: true},
		{name: "essential disguised operation", prefix: "WARNING: The following essential packages will be removed.\n  Remv gone\n", bad: true},
		{name: "wrong listed architecture", prefix: "The following NEW packages will be installed:\n  app:arm64\n", bad: true},
		{name: "stray indented action", prefix: "  Inst app\n", bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.prefix + combined
			f := simulationFixture(out)
			got, err := New(f).Simulate(context.Background(), []string{"app", "old"})
			if (err != nil) != tc.bad || got.Unresolved != tc.bad {
				t.Fatalf("preview=%+v err=%v", got, err)
			}
			if got.Simulation.Stdout != out || len(got.Operations) < 7 {
				t.Fatal("lost records")
			}
		})
	}
}

func TestSimulationMetadataDiagnosticNeverSilentlySafe(t *testing.T) {
	for _, tc := range []struct{ name, out, stderr string }{
		{name: "error on stdout", out: "E: incomplete record set\n"},
		{name: "unknown warning on stdout", out: "W: unsupported metadata warning\n"},
		{name: "essential warning outside simulation", out: "WARNING: The following essential packages will be removed.\n"},
		{name: "error on stderr", stderr: "E: incomplete metadata\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := simulationFixture(combined)
			r := f.results["apt-cache show -- app:amd64=2"]
			r.out += tc.out
			r.stderr = tc.stderr
			f.results["apt-cache show -- app:amd64=2"] = r
			got, err := New(f).Simulate(context.Background(), []string{"app", "old"})
			if err == nil || !got.Unresolved || len(got.Operations) < 7 {
				t.Fatal("diagnostic ignored")
			}
		})
	}
}

func TestSimulationStderrNeverSilentlySafe(t *testing.T) {
	for _, stderr := range []string{"E: solver reports a failure\n", "W: unfamiliar risk\n", "WARNING: unsupported warning\n"} {
		f := simulationFixture(combined)
		r := f.results[simCommand]
		r.stderr = stderr
		f.results[simCommand] = r
		got, err := New(f).Simulate(context.Background(), []string{"app", "old"})
		if err == nil || !got.Unresolved || got.Simulation.Stderr != stderr || got.Simulation.Stdout != combined || len(got.Operations) != 8 || got.Operations[7].Kind != "unresolved" {
			t.Fatalf("preview=%+v err=%v", got, err)
		}
		if got.Simulation.Err == nil || !strings.Contains(got.Simulation.Err.Error(), "unexpected stderr from apt-get") || !strings.Contains(err.Error(), "unexpected stderr from apt-get") {
			t.Fatalf("stderr rejection missing: command error=%v, error=%v", got.Simulation.Err, err)
		}
	}
}

func TestSimulationHeadingRiskIsPreserved(t *testing.T) {
	for _, tc := range []struct {
		name, prefix, packageName, risk string
		bad                             bool
	}{
		{name: "held change", prefix: "The following held packages will be changed:\n  app\n", packageName: "app", risk: "review-required"},
		{name: "essential contradicts metadata", prefix: "WARNING: The following essential packages will be removed.\nThis should NOT be done unless you know exactly what you are doing!\n  gone\n", packageName: "gone", risk: "high", bad: true},
		{name: "essential agrees with metadata", prefix: "WARNING: The following essential packages will be removed.\n  gone\n", packageName: "gone", risk: "high"},
		{name: "essential empty", prefix: "WARNING: The following essential packages will be removed.\n", risk: "high", bad: true},
		{name: "held empty", prefix: "The following held packages will be changed:\n", bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := simulationFixture(tc.prefix + combined)
			if tc.name == "essential agrees with metadata" {
				f.results["dpkg-query -W -f="+metadataFormat+" -- gone"] = result{out: record("gone", "1", "Status: install ok installed\nEssential: yes\nProtected: no")}
			}
			got, err := New(f).Simulate(context.Background(), []string{"app", "old"})
			if (err != nil) != tc.bad {
				t.Fatalf("preview=%+v err=%v", got, err)
			}
			found := tc.packageName == ""
			for _, op := range got.Operations {
				if tc.packageName != "" && op.Package == tc.packageName {
					found = true
					if op.Risk != tc.risk {
						t.Fatalf("risk lost %+v", op)
					}
				}
			}
			if tc.name == "essential empty" && !strings.Contains(strings.Join(got.ReviewRequirements, "\n"), "(high)") {
				t.Fatal("missing high-risk unresolved warning")
			}
			if !found || !strings.Contains(strings.Join(got.Warnings, "\n"), strings.Split(tc.prefix, "\n")[0]) || len(got.ReviewRequirements) == 0 {
				t.Fatalf("heading evidence lost %+v", got)
			}
		})
	}
}
