package apt

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type result struct {
	out, stderr string
	err         error
}
type commandError int

func (e commandError) Error() string { return "command failed" }
func (e commandError) ExitCode() int { return int(e) }

type fakeRunner struct {
	results map[string]result
	calls   []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	key := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, key)
	r, ok := f.results[key]
	if !ok {
		return "", "", errors.New("unexpected command: " + key)
	}
	return r.out, r.stderr, r.err
}

func TestInspectDerivesInstalledUpdateAndNotFound(t *testing.T) {
	cases := []struct {
		status, version, candidate string
		statusErr, policyErr       error
		want                       string
	}{
		{"ii ", "1.0", "2.0", nil, nil, "update-available"},
		{"", "", "(none)", commandError(1), nil, "unavailable"},
	}
	for _, tc := range cases {
		out := tc.status + "\t" + tc.version + "\n"
		if tc.statusErr != nil {
			out = ""
		}
		f := &fakeRunner{results: map[string]result{
			"dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- vim": {out: out, stderr: "dpkg-query: no packages found matching vim\n", err: tc.statusErr},
			"apt-cache policy vim":               {out: "vim:\n  Installed: " + tc.version + "\n  Candidate: " + tc.candidate + "\n", err: tc.policyErr},
			"dpkg --compare-versions 1.0 ge 2.0": {out: "", err: commandError(1)},
		}}
		provider := New(f)
		got, err := provider.Inspect(context.Background(), "vim")
		if err != nil {
			t.Fatal(err)
		}
		if string(got.Status) != tc.want {
			t.Errorf("status = %q, want %q", got.Status, tc.want)
		}
	}
}

func TestInspectTreatsEmptyPolicyAsNotFound(t *testing.T) {
	f := &fakeRunner{results: map[string]result{
		"dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- missing": {stderr: "dpkg-query: no packages found matching missing\n", err: commandError(1)},
		"apt-cache policy missing":                                        {},
	}}
	got, err := New(f).Inspect(context.Background(), "missing")
	if err != nil || got.Status != "unavailable" {
		t.Fatalf("missing package = %#v, %v; want unavailable", got, err)
	}
}

func TestInspectReturnsToolFailures(t *testing.T) {
	f := &fakeRunner{results: map[string]result{
		"dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- vim": {err: errors.New("database read failure")},
	}}
	if _, err := New(f).Inspect(context.Background(), "vim"); err == nil || !strings.Contains(err.Error(), "installed package") {
		t.Fatalf("expected dpkg-query failure, got %v", err)
	}
}

func TestRegressionAPTStates(t *testing.T) {
	for _, tc := range []struct {
		name, status, installed, candidate, want string
		compareErr                               error
	}{
		{"held installed", "hi ", "2", "2", "current", nil},
		{"current", "ii ", "2", "2", "current", nil},
		{"older candidate", "ii ", "2", "1", "current", nil},
		{"no candidate", "ii ", "2", "(none)", "unknown", nil},
		{"comparison failure", "ii ", "2", "3", "", commandError(2)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{results: map[string]result{
				"dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- vim": {out: tc.status + "\t" + tc.installed + "\n"},
				"apt-cache policy vim": {out: "vim:\n  Candidate: " + tc.candidate + "\n"},
				"dpkg --compare-versions " + tc.installed + " ge " + tc.candidate: {err: tc.compareErr},
			}}
			got, err := New(f).Inspect(context.Background(), "vim")
			if tc.want == "" {
				if err == nil || !strings.Contains(err.Error(), "compare Debian versions") {
					t.Fatalf("expected comparison failure, got %#v, %v", got, err)
				}
				return
			}
			if err != nil || string(got.Status) != tc.want || got.InstalledVersion != tc.installed {
				t.Fatalf("got %#v, %v; want %s installed %s", got, err, tc.want, tc.installed)
			}
		})
	}
}

func TestRegressionAPTRejectsMalformedOutput(t *testing.T) {
	for _, tc := range []struct{ name, query, policy string }{
		{"short status", "ii\t1\n", "Candidate: 2\n"},
		{"invalid desired", "xi \t1\n", "Candidate: 2\n"},
		{"invalid actual", "ix \t1\n", "Candidate: 2\n"},
		{"invalid error flag", "iiX\t1\n", "Candidate: 2\n"},
		{"requires reinstall", "iiR\t1\n", "Candidate: 2\n"},
		{"missing installed version", "ii \t\n", "Candidate: 2\n"},
		{"multiple records", "ii \t1\nii \t2\n", "Candidate: 2\n"},
		{"empty candidate", "ii \t1\n", "vim:\n  Candidate: \n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{results: map[string]result{
				"dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- vim": {out: tc.query},
				"apt-cache policy vim":                   {out: tc.policy},
				"dpkg --compare-versions 1 ge 2":         {err: commandError(1)},
				"dpkg --compare-versions 1\nii \t2 ge 2": {err: commandError(1)},
			}}
			if got, err := New(f).Inspect(context.Background(), "vim"); err == nil {
				t.Fatalf("accepted malformed output: %#v", got)
			}
		})
	}
}

func TestRegressionAPTDoesNotSwallowUnknownExitOne(t *testing.T) {
	// Synthetic operational diagnostic, not a claim about a real host failure.
	for _, diagnostic := range []string{"", "dpkg-query: database read failed", "dpkg-query: no packages found matching other"} {
		f := &fakeRunner{results: map[string]result{
			"dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- vim": {err: commandError(1), stderr: diagnostic},
			"apt-cache policy vim": {},
		}}
		if _, err := New(f).Inspect(context.Background(), "vim"); err == nil {
			t.Errorf("exit 1 swallowed for diagnostic %q", diagnostic)
		}
		if len(f.calls) != 1 {
			t.Errorf("continued after query failure: %v", f.calls)
		}
	}
}

func TestInspectRejectsUnsafePackageOperand(t *testing.T) {
	f := &fakeRunner{results: map[string]result{}}
	if _, err := New(f).Inspect(context.Background(), "-oAPT::Foo=bar"); err == nil {
		t.Fatal("unsafe operand accepted")
	}
	if len(f.calls) != 0 {
		t.Fatalf("commands executed for invalid operand: %v", f.calls)
	}
}
