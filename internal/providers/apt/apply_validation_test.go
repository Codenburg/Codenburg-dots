package apt

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func installPreview() Preview {
	return Preview{Requests: []string{"demo"}, Operations: []Operation{{Kind: "install", Package: "demo", Architecture: "amd64", NewVersion: "2", Requested: true, Risk: "normal", Candidate: PackageEvidence{Known: true, Package: "demo", Architecture: "amd64", Version: "2"}}}}
}
func upgradeOperation() Operation {
	return Operation{Kind: "upgrade", Package: "libdemo", Architecture: "amd64", OldVersion: "1", NewVersion: "2", Risk: "review-required", Installed: PackageEvidence{Known: true, Package: "libdemo", Architecture: "amd64", Version: "1", Status: "install ok installed"}, Candidate: PackageEvidence{Known: true, Package: "libdemo", Architecture: "amd64", Version: "2"}}
}
func TestValidateApply(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Preview)
		good   bool
	}{
		{name: "install", good: true},
		{name: "indirect upgrade", good: true, change: func(v *Preview) { v.Operations = append(v.Operations, upgradeOperation()) }},
		{name: "critical upgrade allowed for specific review", good: true, change: func(v *Preview) {
			op := upgradeOperation()
			op.Installed.Essential = true
			v.Operations = append(v.Operations, op)
		}},
		{name: "removal", good: true, change: func(v *Preview) {
			op := upgradeOperation()
			op.Kind = "removal"
			op.NewVersion = ""
			op.Candidate = PackageEvidence{}
			v.Operations = append(v.Operations, op)
		}},
		{name: "unresolved", change: func(v *Preview) { v.Unresolved = true }},
		{name: "uncertain", change: func(v *Preview) { v.Operations[0].Uncertainty = []string{"unknown"} }},
		{name: "unknown candidate", change: func(v *Preview) { v.Operations[0].Candidate.Known = false }},
		{name: "unbound candidate version", change: func(v *Preview) { v.Operations[0].Candidate.Version = "3" }},
		{name: "unbound candidate architecture", change: func(v *Preview) { v.Operations[0].Candidate.Architecture = "all" }},
		{name: "unbound candidate package", change: func(v *Preview) { v.Operations[0].Candidate.Package = "other" }},
		{name: "missing architecture", change: func(v *Preview) { v.Operations[0].Architecture = "" }},
		{name: "missing version", change: func(v *Preview) { v.Operations[0].NewVersion = "" }},
		{name: "unknown risk", change: func(v *Preview) { v.Operations[0].Risk = "" }},
		{name: "contradictory request", change: func(v *Preview) { v.Operations[0].Requested = false }},
		{name: "missing requested install", change: func(v *Preview) { v.Operations = []Operation{upgradeOperation()} }},
		{name: "duplicate actions", change: func(v *Preview) { v.Operations = append(v.Operations, v.Operations[0]) }},
		{name: "unknown installed", change: func(v *Preview) {
			op := upgradeOperation()
			op.Installed.Known = false
			v.Operations = append(v.Operations, op)
		}},
		{name: "bound installed version", change: func(v *Preview) {
			op := upgradeOperation()
			op.Installed.Version = "0"
			v.Operations = append(v.Operations, op)
		}},
		{name: "held", change: func(v *Preview) {
			op := upgradeOperation()
			op.Installed.Held = true
			v.Operations = append(v.Operations, op)
		}},
		{name: "contradictory hold status", change: func(v *Preview) {
			op := upgradeOperation()
			op.Installed.Status = "hold ok installed"
			v.Operations = append(v.Operations, op)
		}},
		{name: "partial status", change: func(v *Preview) {
			op := upgradeOperation()
			op.Installed.Status = "install ok unpacked"
			v.Operations = append(v.Operations, op)
		}},
		{name: "downgrade", change: func(v *Preview) {
			op := upgradeOperation()
			op.NewVersion = "0"
			op.Candidate.Version = "0"
			v.Operations = append(v.Operations, op)
		}},
		{name: "essential installed removal candidate false", change: func(v *Preview) {
			op := upgradeOperation()
			op.Kind = "removal"
			op.NewVersion = ""
			op.Candidate = PackageEvidence{}
			op.Installed.Essential = true
			v.Operations = append(v.Operations, op)
		}},
		{name: "protected installed removal candidate false", change: func(v *Preview) {
			op := upgradeOperation()
			op.Kind = "removal"
			op.NewVersion = ""
			op.Candidate = PackageEvidence{}
			op.Installed.Protected = true
			v.Operations = append(v.Operations, op)
		}},
		{name: "paired configuration", good: true, change: func(v *Preview) {
			op := v.Operations[0]
			op.Kind = "configuration"
			v.Operations = append(v.Operations, op)
		}},
		{name: "contradictory configuration", change: func(v *Preview) {
			op := v.Operations[0]
			op.Kind = "configuration"
			op.Candidate.Protected = true
			v.Operations = append(v.Operations, op)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := installPreview()
			if tc.change != nil {
				tc.change(&v)
			}
			runner := &fakeRunner{results: map[string]result{"dpkg --compare-versions 2 gt 1": {}, "dpkg --compare-versions 0 gt 1": {err: commandError(1)}}}
			err := New(runner).ValidateApply(context.Background(), v)
			if (err == nil) != tc.good {
				t.Fatalf("good=%v err=%v", tc.good, err)
			}
		})
	}
}

func TestInspectApplyStateBindsIndependentProtection(t *testing.T) {
	r := &fakeRunner{results: map[string]result{
		"dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- demo:amd64": {out: "ii \t1\n"},
		"apt-cache policy demo:amd64":                                        {out: "Candidate: 2\n"},
		"dpkg --compare-versions 1 ge 2":                                     {err: commandError(1)},
		"dpkg-query --status -- demo:amd64":                                  {out: "Package: demo\nArchitecture: amd64\nVersion: 1\nStatus: install ok installed\nProtected: yes\n"},
		"apt-cache show -- demo:amd64=2":                                     {out: "Package: demo\nArchitecture: amd64\nVersion: 2\nProtected: no\n"},
	}}
	state, err := New(r).InspectApplyState(context.Background(), "demo:amd64")
	if err != nil || !state.Installed.Known || !state.Installed.Protected || !state.Candidate.Known || state.Candidate.Protected {
		t.Fatalf("%+v %v", state, err)
	}
	r.results["apt-cache show -- demo:amd64=2"] = result{out: "Package: demo\nArchitecture: all\nVersion: 2\n"}
	if _, err = New(r).InspectApplyState(context.Background(), "demo:amd64"); err == nil {
		t.Fatal("unbound candidate accepted")
	}
}

func TestCompareVersionsDirectArguments(t *testing.T) {
	sentinel := errors.New("dpkg unavailable")
	cases := []struct {
		name, left, right string
		response          result
		want, fail        bool
	}{
		{name: "Debian epoch", left: "1:1.0", right: "9.0", want: true},
		{name: "Debian tilde", left: "1.0~rc1", right: "1.0", response: result{err: commandError(1)}},
		{name: "failure", left: "1", right: "2", response: result{err: sentinel}, fail: true},
		{name: "diagnostic", left: "1", right: "2", response: result{stderr: "warning"}, fail: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := "dpkg --compare-versions " + tc.left + " gt " + tc.right
			r := &fakeRunner{results: map[string]result{key: tc.response}}
			got, err := New(r).CompareVersions(context.Background(), tc.left, "gt", tc.right)
			if got != tc.want || (err != nil) != tc.fail || len(r.calls) != 1 || r.calls[0] != key {
				t.Fatalf("got=%v err=%v calls=%v", got, err, r.calls)
			}
			if tc.name == "failure" && !errors.Is(err, sentinel) {
				t.Fatal("lost error chain")
			}
		})
	}
	r := &fakeRunner{}
	if _, err := New(r).CompareVersions(context.Background(), "1; echo bad", "gt", "2"); err == nil || len(r.calls) != 0 {
		t.Fatal("unsafe operand accepted")
	}
}

func TestSameTransaction(t *testing.T) {
	a := installPreview()
	a.Operations = append(a.Operations, upgradeOperation())
	b := installPreview()
	b.Operations = append([]Operation{upgradeOperation()}, b.Operations...)
	b.Simulation.Stdout = "different diagnostic"
	b.Warnings = []string{"different prose"}
	b.ReviewRequirements = []string{"format change"}
	if !SameTransaction(a, b) {
		t.Fatal("diagnostics or order affected identity")
	}
	for _, tc := range []struct {
		name   string
		change func(*Preview)
	}{
		{name: "version", change: func(v *Preview) { v.Operations[0].NewVersion = "3" }},
		{name: "architecture", change: func(v *Preview) { v.Operations[0].Architecture = "all" }},
		{name: "installed protection", change: func(v *Preview) { v.Operations[0].Installed.Protected = true }},
		{name: "candidate protection", change: func(v *Preview) { v.Operations[0].Candidate.Essential = true }},
		{name: "status", change: func(v *Preview) { v.Operations[0].Installed.Status = "hold ok installed" }},
		{name: "holds", change: func(v *Preview) { v.Operations[0].Installed.Held = true }},
		{name: "requests", change: func(v *Preview) { v.Requests = []string{"other"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := b
			v.Operations = append([]Operation{}, b.Operations...)
			tc.change(&v)
			if SameTransaction(a, v) {
				t.Fatal("drift ignored")
			}
		})
	}
}

func TestInspectApplyRejectsPartialAbsence(t *testing.T) {
	for _, status := range []string{"install ok unpacked", "install ok half-configured", "deinstall ok config-files"} {
		t.Run(status, func(t *testing.T) {
			r := &fakeRunner{results: map[string]result{
				"dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- demo": {out: "iU \t1\n"},
				"apt-cache policy demo": {out: "Candidate: 2\n"},
				"dpkg-query -W -f=${Status}\\t${Version}\\t${Architecture}\\n -- demo": {out: status + "\t1\tamd64\n"},
			}}
			_, err := New(r).InspectApply(context.Background(), "demo")
			if (err == nil) != strings.Contains(status, "config-files") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
