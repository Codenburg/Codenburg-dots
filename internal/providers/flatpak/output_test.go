package flatpak

import (
	"context"
	"errors"
	"testing"
)

func TestMalformedEvidence(t *testing.T) {
	cases := []struct {
		name string
		op   Operation
		text string
	}{
		{name: "spaces instead of tabs", op: List, text: "org.example.App/x86_64/stable flathub 1\n"},
		{name: "missing origin field", op: List, text: "org.example.App/x86_64/stable\n"},
		{name: "empty arch", op: List, text: "org.example.App//stable\tflathub\t1\n"},
		{name: "empty origin", op: List, text: "org.example.App/x86_64/stable\t\t1\n"},
		{name: "runtime", op: List, text: "runtime/org.example.App/x86_64/stable\tflathub\t1\n"},
		{name: "extension runtime", op: List, text: "runtime/org.example.App.Locale/x86_64/stable\tflathub\t1\n"},
		{name: "control byte", op: List, text: "org.example.App/x86_64/stable\tflathub\t1\r\n"},
		{name: "invalid UTF-8 version", op: List, text: "org.example.App/x86_64/stable\tflathub\t\xff\n"},
		{name: "invalid UTF-8 remote options", op: Remotes, text: "flathub\t\xff\n"},
		{name: "extra field", op: List, text: "org.example.App/x86_64/stable\tflathub\t1\textra\n"},
		{name: "remote header", op: Remotes, text: "Name\tOptions\n"},
		{name: "remote URL", op: Remotes, text: "https://evil\t\n"},
		{name: "remote empty name", op: Remotes, text: "\t\n"},
		{name: "remote unknown option", op: Remotes, text: "flathub\tmystery\n"},
		{name: "remote duplicate", op: Remotes, text: "flathub\t\nflathub\tdisabled\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture()
			f.rows[Query{Operation: tc.op, Scope: System}] = tc.text
			got, err := New(f).Inspect(context.Background(), Selection{ID: "org.example.App", Scope: System})
			if err != nil {
				t.Fatal(err)
			}
			if got.State != Unknown || len(got.Diagnostics) == 0 {
				t.Fatalf("malformed data not diagnosed: %+v", got)
			}
		})
	}
}

// Flatpak's non-fancy table printer chomps trailing empty cells and emits a
// final newline even for an empty table. Exercise that shape through the provider.
func TestOmittedTrailingColumns(t *testing.T) {
	cases := []struct {
		name, remotes, installed, candidate string
		state                               State
		wantRefs                            int
	}{
		{name: "empty table newline", remotes: "\n", installed: "\n", state: Unavailable},
		{name: "remote empty options", remotes: "flathub\n", installed: "\n", candidate: "\n", state: Unavailable},
		{name: "installed unknown version", remotes: "flathub\n", installed: "org.example.App/x86_64/stable\tflathub\n", candidate: "\n", state: Installed, wantRefs: 1},
		{name: "candidate unknown version", remotes: "flathub\n", installed: "\n", candidate: "org.example.App/x86_64/stable\n", state: Available, wantRefs: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture()
			f.rows[Query{Operation: Remotes, Scope: System}] = tc.remotes
			f.rows[Query{Operation: List, Scope: System}] = tc.installed
			f.rows[Query{Operation: RemoteLS, Scope: System, Remote: "flathub"}] = tc.candidate
			got, err := New(f).Inspect(context.Background(), Selection{ID: "org.example.App", Scope: System})
			if err != nil || got.State != tc.state || len(got.Diagnostics) != 0 {
				t.Fatalf("state/evidence: %+v, error: %v", got, err)
			}
			refs := append(got.Installed, got.Candidates...)
			if len(refs) != tc.wantRefs {
				t.Fatalf("refs: %+v", refs)
			}
			for _, ref := range refs {
				if ref.ID != "org.example.App" || ref.Scope != System || ref.Remote != "flathub" || ref.Arch != "x86_64" || ref.Branch != "stable" || ref.Version != "" {
					t.Fatalf("identity/version altered: %+v", ref)
				}
			}
			wantCalls := 2
			if tc.remotes != "\n" {
				wantCalls++
			}
			if len(f.calls) != wantCalls {
				t.Fatalf("query count %d, want %d", len(f.calls), wantCalls)
			}
		})
	}
}

func TestTableInvalidUTF8(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{name: "invalid leading byte", text: "app\t\xff\n"},
		{name: "truncated multibyte sequence", text: "app\t\xe2\x82\n"},
		{name: "valid replacement character", text: "app\t\ufffd\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, diagnostics := table(tc.text, 2)
			if tc.name == "valid replacement character" {
				if len(rows) != 1 || len(diagnostics) != 0 {
					t.Fatalf("valid UTF-8 rejected: %v, %v", rows, diagnostics)
				}
			} else if len(rows) != 0 || len(diagnostics) != 1 {
				t.Fatalf("invalid UTF-8 accepted: %v, %v", rows, diagnostics)
			}
		})
	}
}

func TestAlternateEvidence(t *testing.T) {
	t.Run("disabled remotes never queried", func(t *testing.T) {
		f := fixture()
		f.rows[Query{Operation: Remotes, Scope: System}] = "flathub\tdisabled,no-enumerate\n"
		got, err := New(f).Inspect(context.Background(), Selection{ID: "org.example.App", Scope: System})
		if err != nil || got.State != Unavailable {
			t.Fatalf("%+v %v", got, err)
		}
		for _, q := range f.calls {
			if q.Operation == RemoteLS {
				t.Fatal("queried disabled remote")
			}
		}
	})
	t.Run("explicit user only", func(t *testing.T) {
		f := fixture()
		f.rows[Query{Operation: List, Scope: User}] = "org.example.App/x86_64/beta\tflathub\t9 beta\n"
		got, err := New(f).Inspect(context.Background(), Selection{ID: "org.example.App", Scope: User})
		if err != nil || got.State != Installed || got.Installed[0].Version != "9 beta" {
			t.Fatalf("%+v %v", got, err)
		}
		for _, q := range f.calls {
			if q.Scope != User {
				t.Fatal("scope leaked")
			}
		}
	})
	t.Run("named availability", func(t *testing.T) {
		f := fixture()
		f.rows[Query{Operation: RemoteLS, Scope: "installation:work", Remote: "flathub"}] = "org.example.App/aarch64/edge\t\n"
		got, err := New(f).Inspect(context.Background(), Selection{ID: "org.example.App", Scope: "installation:work"})
		if err != nil || got.State != Available || got.Candidates[0].Scope != "installation:work" {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("remote identities", func(t *testing.T) {
		f := fixture()
		f.rows[Query{Operation: Remotes, Scope: System}] = "second\t\nflathub\t\n"
		for _, remote := range []string{"second", "flathub"} {
			f.rows[Query{Operation: RemoteLS, Scope: System, Remote: remote}] = "org.example.App/x86_64/stable\t1\n"
		}
		got, err := New(f).Inspect(context.Background(), Selection{ID: "org.example.App", Scope: System})
		if err != nil || got.State != Ambiguous || got.Candidates[0].Remote != "flathub" {
			t.Fatalf("%+v %v", got, err)
		}
		got, err = New(f).Inspect(context.Background(), Selection{ID: "org.example.App", Scope: System, Remote: "second"})
		if err != nil || got.State != Available {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("conflicting versions", func(t *testing.T) {
		f := fixture()
		f.rows[Query{Operation: List, Scope: System}] = "org.example.App/x86_64/stable\tflathub\t1\norg.example.App/x86_64/stable\tflathub\t2\n"
		got, err := New(f).Inspect(context.Background(), Selection{ID: "org.example.App", Scope: System})
		if err != nil || got.State != Installed || len(got.Installed) != 1 || got.Installed[0].Version != "" || len(got.Diagnostics) == 0 {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("list failure retains candidate uncertainty", func(t *testing.T) {
		f := fixture()
		f.failures[Query{Operation: List, Scope: System}] = errors.New("read denied")
		f.rows[Query{Operation: RemoteLS, Scope: System, Remote: "flathub"}] = "org.example.App/x86_64/stable\t1\n"
		got, err := New(f).Inspect(context.Background(), Selection{ID: "org.example.App", Scope: System})
		if err != nil || got.State != Unknown || len(got.Candidates) != 1 {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("cancelled context preserved", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		got, err := New(fixture()).Inspect(ctx, Selection{ID: "org.example.App", Scope: System})
		if err != nil || got.State != Unknown || len(got.Diagnostics) != 2 {
			t.Fatalf("%+v %v", got, err)
		}
		if !errors.Is(got.Diagnostics[0].Err, context.Canceled) {
			t.Fatal("lost cancellation")
		}
	})
	t.Run("nil runner fails closed", func(t *testing.T) {
		if _, err := New(nil).Inventory(context.Background(), ""); err == nil {
			t.Fatal("nil runner accepted")
		}
	})
	t.Run("inventory defaults system", func(t *testing.T) {
		f := fixture()
		if _, err := New(f).Inventory(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
		for _, q := range f.calls {
			if q.Scope != System {
				t.Fatal("wrong default")
			}
		}
	})
}
