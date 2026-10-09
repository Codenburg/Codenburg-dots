package apt

import (
	"context"
	"strings"
	"testing"
)

func TestSimulateOptionalFlagsAndInstalledHold(t *testing.T) {
	f := simulationFixture(combined)
	f.results["apt-cache show -- app:amd64=2"] = result{out: record("app", "2", "Description: optional flags omitted")}
	f.results["dpkg-query --status -- old:amd64"] = result{out: record("old", "1", "Status: hold ok installed")}
	got, err := New(f).Simulate(context.Background(), []string{"app", "old"})
	if err != nil || got.Unresolved {
		t.Fatalf("valid optional omissions unresolved: %v", err)
	}
	for _, op := range got.Operations {
		if op.Package == "old" {
			want := PackageEvidence{Package: "old", Architecture: "amd64", Version: "1", Known: true,
				Status: "hold ok installed", Held: true}
			if op.Risk != "review-required" || op.Installed != want || !op.Candidate.Known {
				t.Fatalf("installed hold not retained independently: %+v", op)
			}
		}
		if op.Package == "app" && (!op.Candidate.Known || op.Candidate.Essential || op.Candidate.Protected) {
			t.Fatalf("omission not known false: %+v", op)
		}
	}
}

func TestTransactionMetadataRecordValidation(t *testing.T) {
	valid := record("old", "1", "Status: install ok installed")
	for _, tc := range []struct {
		name string
		r    result
	}{
		{name: "empty query"},
		{name: "failed query with complete stdout", r: result{out: valid, err: commandError(1)}},
		{name: "stderr with complete stdout", r: result{out: valid, stderr: "warning"}},
		{name: "missing package", r: result{out: strings.Replace(valid, "Package: old\n", "", 1)}},
		{name: "missing version", r: result{out: strings.Replace(valid, "Version: 1\n", "", 1)}},
		{name: "missing architecture", r: result{out: strings.Replace(valid, "Architecture: amd64\n", "", 1)}},
		{name: "missing status", r: result{out: record("old", "1", "Description: missing status")}},
		{name: "empty essential", r: result{out: valid + "Essential:\n"}},
		{name: "empty protected", r: result{out: valid + "Protected: \n"}},
		{name: "malformed flag", r: result{out: valid + "Essential: maybe\n"}},
		{name: "duplicate flags", r: result{out: valid + "Protected: no\nProtected: no\n"}},
		{name: "conflicting flags", r: result{out: valid + "Essential: yes\nEssential: no\n"}},
		{name: "duplicate record", r: result{out: valid + "\n" + valid}},
		{name: "conflicting record", r: result{out: valid + "\n" + record("old", "2", "Status: hold ok installed")}},
		{name: "contradictory version", r: result{out: strings.Replace(valid, "Version: 1", "Version: 2", 1)}},
		{name: "contradictory architecture", r: result{out: strings.Replace(valid, "amd64", "arm64", 1)}},
		{name: "contradictory identity", r: result{out: strings.Replace(valid, "Package: old", "Package: other", 1)}},
		{name: "duplicate identity", r: result{out: valid + "Package: old\n"}},
		{name: "case insensitive duplicate", r: result{out: valid + "pRoTeCtEd: yes\nProtected: no\n"}},
		{name: "continued flag", r: result{out: valid + "Essential: no\n yes\n"}},
		{name: "continued version", r: result{out: "Package: old\nVersion: 1\n 2\nArchitecture: amd64\nStatus: install ok installed\n"}},
		{name: "orphan continuation", r: result{out: " orphan\n" + valid}},
		{name: "malformed field", r: result{out: valid + "broken\n"}},
		{name: "partial status", r: result{out: strings.Replace(valid, "install ok installed", "install ok unpacked", 1)}},
		{name: "error status", r: result{out: strings.Replace(valid, "install ok installed", "hold reinstreq installed", 1)}},
		{name: "unsupported selection", r: result{out: strings.Replace(valid, "install ok installed", "deinstall ok installed", 1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{results: map[string]result{"dpkg-query --status -- old:amd64": tc.r}}
			m := New(f).transactionMetadata(context.Background(), "old:amd64", "1", true)
			if m.err == nil || m.evidence.Known {
				t.Fatalf("invalid record promoted to known evidence: %+v", m)
			}
		})
	}
}

func TestTransactionMetadataOptionalFlags(t *testing.T) {
	for _, old := range []bool{false, true} {
		for _, tc := range []struct {
			name, flags          string
			essential, protected bool
		}{
			{name: "omitted"},
			{name: "explicit no", flags: "Essential: no\nProtected: no"},
			{name: "essential", flags: "Essential: yes", essential: true},
			{name: "protected", flags: "Protected: yes", protected: true},
			{name: "both", flags: "Essential: yes\nProtected: yes", essential: true, protected: true},
			{name: "case insensitive tags", flags: "eSsEnTiAl: yes\npRoTeCtEd: no", essential: true},
		} {
			prefix := "candidate/"
			key := "apt-cache show -- old:amd64=1"
			flags := tc.flags
			status := ""
			if old {
				prefix = "installed/"
				key = "dpkg-query --status -- old:amd64"
				status = "install ok installed"
				flags = "Status: " + status + "\n" + flags
			}
			t.Run(prefix+tc.name, func(t *testing.T) {
				flags = strings.TrimSpace(flags)
				if flags != "" {
					flags += "\n"
				}
				out := record("old", "1", flags+"Description: summary\n long description\nConffiles:\n /etc/file hash")
				f := &fakeRunner{results: map[string]result{key: {out: out}}}
				m := New(f).transactionMetadata(context.Background(), "old:amd64", "1", old)
				want := PackageEvidence{Package: "old", Architecture: "amd64", Version: "1", Known: true,
					Essential: tc.essential, Protected: tc.protected, Status: status}
				if m.err != nil || m.evidence != want || m.critical != (tc.essential || tc.protected) {
					t.Fatalf("metadata=%+v want=%+v", m, want)
				}
			})
		}
	}
}

func TestSimulateInstalledAndCandidateProtectionAreIndependent(t *testing.T) {
	f := simulationFixture(combined)
	f.results["dpkg-query --status -- old:amd64"] = result{out: record("old", "1", "Status: install ok installed\nProtected: yes")}
	f.results["apt-cache show -- old:amd64=2"] = result{out: record("old", "2", "Essential: yes")}
	got, err := New(f).Simulate(context.Background(), []string{"app", "old"})
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range got.Operations {
		if op.Package == "old" && (!op.Installed.Known || !op.Candidate.Known ||
			!op.Installed.Protected || op.Installed.Essential || !op.Candidate.Essential || op.Candidate.Protected ||
			op.Installed.Version != "1" || op.Candidate.Version != "2" || op.Risk != "high") {
			t.Fatalf("installed and candidate evidence conflated: %+v", op)
		}
	}
}
