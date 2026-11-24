package apt

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestValidateApplyAbsentRequestedPackage(t *testing.T) {
	for _, tc := range []struct {
		name, operand string
		failure       error
	}{
		{name: "git", operand: "git", failure: commandError(1)},
		{name: "git:amd64", operand: "git:amd64", failure: commandError(1)},
		{name: "wrapped exit", operand: "git", failure: fmt.Errorf("dpkg-query: %w", errors.Join(commandError(1)))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := tc.operand
			query := "dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- " + name
			stderr := "dpkg-query: no packages found matching " + name + "\n"
			absent := tc.failure
			simulation := strings.TrimSuffix(simCommand, "app old") + name
			r := &fakeRunner{results: map[string]result{
				"dpkg --print-architecture":     {out: "amd64\n"},
				query:                           {stderr: stderr, err: absent},
				"apt-cache policy " + name:      {out: "Candidate: 2\n"},
				"apt-cache show -- git:amd64=2": {out: record("git", "2", "Essential: no\nProtected: no")},
				simulation: {out: "0 upgraded, 1 newly installed, 0 to remove and 0 not upgraded.\n" +
					"Inst git (2 Repo [amd64])\nConf git (2 Repo [amd64])\n"},
			}}
			p := New(r)
			preview, err := p.Simulate(context.Background(), []string{name})
			if err != nil || preview.Unresolved {
				t.Fatalf("simulation: %+v, error: %v", preview, err)
			}
			index := -1
			for i, d := range preview.Metadata {
				if d.Command == query {
					index = i
				}
			}
			if index < 0 {
				t.Fatal("missing original inspection diagnostic")
			}
			diagnostic := preview.Metadata[index]
			if diagnostic.Stdout != "" || diagnostic.Stderr != stderr || !errors.Is(diagnostic.Err, absent) {
				t.Fatalf("absence evidence changed: %+v", diagnostic)
			}
			calls := append([]string(nil), r.calls...)
			if err := p.ValidateApply(context.Background(), preview); err != nil {
				t.Fatalf("expected absence rejected after complete simulation: %v", err)
			}
			if !reflect.DeepEqual(diagnostic, preview.Metadata[index]) || !reflect.DeepEqual(calls, r.calls) {
				t.Fatal("validation changed diagnostic evidence or issued commands")
			}
		})
	}
}

func absentInspectionDiagnostic() Diagnostic {
	return Diagnostic{
		Command: "dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- demo",
		Stderr:  "dpkg-query: no packages found matching demo\n",
		Err:     errors.Join(commandError(1)),
	}
}

func TestValidateApplyUnexpectedMetadataFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Diagnostic)
	}{
		{name: "unexpected exit", change: func(d *Diagnostic) { d.Err = commandError(2) }},
		{name: "unavailable command", change: func(d *Diagnostic) { d.Err = errors.New("command unavailable") }},
		{name: "missing absence diagnostic", change: func(d *Diagnostic) { d.Stderr = "" }},
		{name: "permission failure", change: func(d *Diagnostic) { d.Stderr = "dpkg-query: error: permission denied\n" }},
		{name: "unexpected stdout", change: func(d *Diagnostic) { d.Stdout = "partial database record\n" }},
		{name: "additional diagnostic", change: func(d *Diagnostic) { d.Stderr += "database read failed\n" }},
		{name: "unrequested package", change: func(d *Diagnostic) {
			d.Command = strings.ReplaceAll(d.Command, "demo", "other")
			d.Stderr = strings.ReplaceAll(d.Stderr, "demo", "other")
		}},
		{name: "installed safety metadata", change: func(d *Diagnostic) { d.Command = "dpkg-query --status -- demo:amd64" }},
		{name: "candidate metadata", change: func(d *Diagnostic) { d.Command = "apt-cache show -- demo:amd64=2" }},
		{name: "different query format", change: func(d *Diagnostic) {
			d.Command = "dpkg-query -W -f=${Status}\\t${Version}\\t${Architecture}\\n -- demo"
		}},
		{name: "cancellation", change: func(d *Diagnostic) { d.Err = errors.Join(commandError(1), context.Canceled) }},
		{name: "deadline", change: func(d *Diagnostic) { d.Err = errors.Join(commandError(1), context.DeadlineExceeded) }},
		{name: "additional failure", change: func(d *Diagnostic) { d.Err = errors.Join(commandError(1), errors.New("unreliable evidence")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := installPreview()
			d := absentInspectionDiagnostic()
			tc.change(&d)
			v.Metadata = []Diagnostic{d}
			r := &fakeRunner{}
			err := New(r).ValidateApply(context.Background(), v)
			if err == nil || !strings.HasPrefix(err.Error(), "unresolved metadata:") || !errors.Is(err, d.Err) {
				t.Fatalf("expected original metadata failure, got %v", err)
			}
			if len(r.calls) != 0 || !reflect.DeepEqual(v.Metadata[0], d) {
				t.Fatal("rejected evidence caused commands or diagnostic mutation")
			}
		})
	}
}

func TestValidateApplyAbsenceKeepsSafetyGates(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		change     func(*Preview)
	}{
		{name: "essential removal", want: "essential or protected removal forbidden", change: func(v *Preview) {
			op := upgradeOperation()
			op.Kind, op.NewVersion, op.Candidate, op.Installed.Essential = "removal", "", PackageEvidence{}, true
			v.Operations = append(v.Operations, op)
		}},
		{name: "protected removal", want: "essential or protected removal forbidden", change: func(v *Preview) {
			op := upgradeOperation()
			op.Kind, op.NewVersion, op.Candidate, op.Installed.Protected = "removal", "", PackageEvidence{}, true
			v.Operations = append(v.Operations, op)
		}},
		{name: "held package", want: "held or partial installed state forbidden", change: func(v *Preview) {
			op := upgradeOperation()
			op.Installed.Held = true
			v.Operations = append(v.Operations, op)
		}},
		{name: "downgrade", want: "downgrade or reinstall forbidden", change: func(v *Preview) {
			op := upgradeOperation()
			op.NewVersion, op.Candidate.Version = "0", "0"
			v.Operations = append(v.Operations, op)
		}},
		{name: "unknown candidate", want: "missing or contradictory candidate evidence", change: func(v *Preview) { v.Operations[0].Candidate.Known = false }},
		{name: "uncertain metadata", want: "uncertain operation evidence", change: func(v *Preview) { v.Operations[0].Uncertainty = []string{"unknown"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := installPreview()
			v.Metadata = []Diagnostic{absentInspectionDiagnostic()}
			tc.change(&v)
			r := &fakeRunner{results: map[string]result{
				"dpkg --compare-versions 0 gt 1": {err: commandError(1)},
			}}
			if err := New(r).ValidateApply(context.Background(), v); err == nil || err.Error() != tc.want {
				t.Fatalf("wanted safety rejection %q, got %v", tc.want, err)
			}
		})
	}
}
