package flatpak

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fixtureRunner struct {
	rows     map[Query]string
	failures map[Query]error
	calls    []Query
}

func (f *fixtureRunner) Run(ctx context.Context, q Query) (string, error) {
	f.calls = append(f.calls, q)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return f.rows[q], f.failures[q]
}
func fixture() *fixtureRunner {
	f := &fixtureRunner{rows: map[Query]string{}, failures: map[Query]error{}}
	for _, scope := range []Scope{User, System, "installation:work"} {
		f.rows[Query{Operation: Remotes, Scope: scope}] = "flathub\t\n"
	}
	return f
}
func TestInspect(t *testing.T) {
	cases := []struct {
		name                     string
		selection                Selection
		user, system, candidates string
		fail                     bool
		want                     State
		diagnostics              bool
	}{
		{name: "no remotes", selection: Selection{ID: "org.example.App", Scope: System}, want: Unavailable},
		{name: "available blank version", selection: Selection{ID: "org.example.App", Scope: System}, candidates: "org.example.App/x86_64/stable\t\n", want: Available},
		{name: "installed blank version", selection: Selection{ID: "org.example.App", Scope: System}, system: "org.example.App/x86_64/stable\tflathub\t\n", want: Installed},
		{name: "duplicate scopes", selection: Selection{ID: "org.example.App"}, user: "org.example.App/x86_64/stable\tflathub\t1\n", system: "org.example.App/x86_64/stable\tflathub\t2\n", want: Ambiguous},
		{name: "branches", selection: Selection{ID: "org.example.App", Scope: System}, candidates: "org.example.App/x86_64/stable\t1\norg.example.App/x86_64/beta\t2\n", want: Ambiguous},
		{name: "architectures", selection: Selection{ID: "org.example.App", Scope: System}, candidates: "org.example.App/x86_64/stable\t1\norg.example.App/aarch64/stable\t2\n", want: Ambiguous},
		{name: "explicit filters", selection: Selection{ID: "org.example.App", Scope: System, Arch: "aarch64", Branch: "beta", Remote: "flathub"}, candidates: "app/org.example.App/aarch64/beta\t2 beta\norg.example.App/x86_64/stable\t1\n", want: Available},
		{name: "cache failure", selection: Selection{ID: "org.example.App", Scope: System}, fail: true, want: Unknown, diagnostics: true},
		{name: "installed despite cache failure", selection: Selection{ID: "org.example.App", Scope: System}, system: "org.example.App/x86_64/stable\tflathub\t1\n", fail: true, want: Installed, diagnostics: true},
		{name: "partial list", selection: Selection{ID: "org.example.App", Scope: System}, system: "org.example.App/x86_64/stable\tflathub\t1\nbroken\n", want: Installed, diagnostics: true},
		{name: "malformed kind", selection: Selection{ID: "org.example.App", Scope: System}, candidates: "runtime/org.example.App/x86_64/stable\t1\n", want: Unknown, diagnostics: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture()
			f.rows[Query{Operation: List, Scope: User}] = tc.user
			f.rows[Query{Operation: List, Scope: System}] = tc.system
			f.rows[Query{Operation: RemoteLS, Scope: System, Remote: "flathub"}] = tc.candidates
			if tc.name == "no remotes" {
				f.rows[Query{Operation: Remotes, Scope: System}] = ""
			}
			if tc.fail {
				f.failures[Query{Operation: RemoteLS, Scope: System, Remote: "flathub"}] = errors.New("cache missing")
			}
			got, err := New(f).Inspect(context.Background(), tc.selection)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != tc.want {
				t.Fatalf("state %q want %q: %+v", got.State, tc.want, got)
			}
			if tc.diagnostics && len(got.Diagnostics) == 0 {
				t.Fatal("missing uncertainty diagnostic")
			}
		})
	}
}
func TestInventory(t *testing.T) {
	f := fixture()
	f.rows[Query{Operation: List, Scope: "installation:work"}] = "org.example.Z/x86_64/stable\tflathub\t2 beta\napp/org.example.A/aarch64/beta\tflathub\t\n"
	got, err := New(f).Inventory(context.Background(), "installation:work")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Installed) != 2 || got.Installed[0].ID != "org.example.A" || got.Installed[1].Version != "2 beta" {
		t.Fatalf("%+v", got)
	}
	if !reflect.DeepEqual(f.calls, []Query{{Operation: Remotes, Scope: "installation:work"}, {Operation: List, Scope: "installation:work"}}) {
		t.Fatalf("calls %+v", f.calls)
	}
}
func TestValidation(t *testing.T) {
	valid := Ref{Scope: System, Remote: "flathub", ID: "org.example.App", Arch: "aarch64", Branch: "beta"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	incomplete := valid
	incomplete.Arch = ""
	if err := incomplete.Validate(); err == nil {
		t.Fatal("incomplete ref accepted")
	}
	valid.Scope = ""
	if err := valid.Validate(); err == nil {
		t.Fatal("unscoped ref accepted")
	}
	for _, s := range []Selection{{ID: "--help"}, {ID: "org.example.App", Remote: "https://evil"}, {ID: "org.example.App", Scope: "installation:../bad"}, {ID: "org.example.App", Arch: "*"}, {ID: "org.example.App", Branch: "-stable"}, {ID: "org.example.App", Remote: "/tmp/repo"}} {
		t.Run(s.ID+s.Remote+s.Arch+s.Branch+string(s.Scope), func(t *testing.T) {
			f := fixture()
			if _, err := New(f).Inspect(context.Background(), s); err == nil {
				t.Fatal("accepted invalid selection")
			}
			if len(f.calls) != 0 {
				t.Fatal("executed invalid selection")
			}
		})
	}
}
