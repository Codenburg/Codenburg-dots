package software

import (
	"reflect"
	"strings"
	"testing"
)

func fixture(id string, requires ...string) Software {
	return Software{
		ID: id, Name: id, Requires: requires,
		Variants: []Variant{{Provider: APT, Identifier: id}},
	}
}

func TestBuiltinCatalogLookup(t *testing.T) {
	catalog := BuiltinCatalog()
	for _, tc := range []struct{ id, name string }{
		{"git", "Git"}, {"bash", "Bash"}, {"neovim", "Neovim"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			got, err := catalog.Lookup(tc.id)
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != tc.id || got.Name != tc.name || len(got.Requires) != 0 {
				t.Fatalf("unexpected built-in entry: %+v", got)
			}
			want := []Variant{{Provider: APT, Identifier: tc.id}}
			if !reflect.DeepEqual(got.Variants, want) {
				t.Fatalf("variants = %+v; want %+v", got.Variants, want)
			}
		})
	}
	if len(catalog) != 3 {
		t.Fatalf("catalog has %d entries; want 3", len(catalog))
	}
	// A caller's catalog edits must not change future built-in catalogs.
	entry := catalog["git"]
	entry.Variants[0].Identifier = "changed"
	delete(catalog, "bash")
	fresh := BuiltinCatalog()
	if fresh["git"].Variants[0].Identifier != "git" || len(fresh) != 3 {
		t.Fatal("built-in catalog shared mutable state")
	}
}

func TestLookupErrors(t *testing.T) {
	for _, tc := range []struct {
		name           string
		catalog        Catalog
		id, diagnostic string
	}{
		{"unknown", BuiltinCatalog(), "unknown", `unknown software ID "unknown"`},
		{"nil catalog", nil, "git", `unknown software ID "git"`},
		{"empty ID", BuiltinCatalog(), "", "invalid software ID"},
		{"spaces", BuiltinCatalog(), "git bash", "invalid software ID"},
		{"option", BuiltinCatalog(), "--git", "invalid software ID"},
		{"mismatched ID", Catalog{"git": fixture("bash")}, "git", "catalog ID mismatch"},
		{"missing name", Catalog{"git": {ID: "git"}}, "git", "empty display name"},
		{"blank name", Catalog{"git": {ID: "git", Name: " \t\n"}}, "git", "empty display name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.catalog.Lookup(tc.id)
			if err == nil || !strings.Contains(err.Error(), tc.diagnostic) {
				t.Fatalf("error = %v; want %q", err, tc.diagnostic)
			}
		})
	}
}

func TestResolveBuiltinSelection(t *testing.T) {
	got, err := Resolve(BuiltinCatalog(), Desired{IDs: []string{"neovim", "git", "neovim", "bash"}, Provider: APT})
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, got, []string{"neovim", "git", "bash"})
	for _, item := range got {
		if item.Variant.Provider != APT || item.Variant.Identifier != item.Software.ID {
			t.Fatalf("unexpected resolved variant: %+v", item)
		}
	}
}

func TestResolveDependencyOrder(t *testing.T) {
	catalog := Catalog{
		"app":   fixture("app", "right", "left", "right"),
		"right": fixture("right", "base"),
		"left":  fixture("left", "base"),
		"base":  fixture("base"),
		"other": fixture("other"),
	}
	for _, tc := range []struct {
		name      string
		ids, want []string
	}{
		{"declaration order and diamond", []string{"app", "other", "right"}, []string{"base", "right", "left", "app", "other"}},
		{"request order", []string{"other", "left", "app"}, []string{"other", "base", "left", "right", "app"}},
		{"explicit dependency first", []string{"base", "app", "base"}, []string{"base", "right", "left", "app"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i := 0; i < 5; i++ {
				got, err := Resolve(catalog, Desired{IDs: tc.ids, Provider: APT})
				if err != nil {
					t.Fatal(err)
				}
				assertIDs(t, got, tc.want)
			}
		})
	}
}

func TestResolveReferenceErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		catalog    Catalog
		ids        []string
		diagnostic string
	}{
		{"unknown root", BuiltinCatalog(), []string{"git", "absent"}, `unknown software ID "absent"`},
		{"invalid root", BuiltinCatalog(), []string{""}, "invalid software ID"},
		{"broken reference", Catalog{"app": fixture("app", "absent")}, []string{"app"}, `software "app" requires "absent": unknown software ID "absent"`},
		{"invalid reference", Catalog{"app": fixture("app", "")}, []string{"app"}, `software "app" requires "": invalid software ID`},
		{"self cycle", Catalog{"app": fixture("app", "app")}, []string{"app"}, "dependency cycle: app -> app"},
		{"long cycle", Catalog{"app": fixture("app", "left"), "left": fixture("left", "right"), "right": fixture("right", "left")}, []string{"app"}, "dependency cycle: left -> right -> left"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(tc.catalog, Desired{IDs: tc.ids, Provider: APT})
			if err == nil || !strings.Contains(err.Error(), tc.diagnostic) {
				t.Fatalf("error = %v; want %q", err, tc.diagnostic)
			}
			if got != nil {
				t.Fatalf("partial result on error: %+v", got)
			}
		})
	}
}

func TestResolveVariants(t *testing.T) {
	future := Variant{Provider: Provider("flatpak"), Identifier: "org.example.App"}
	for _, tc := range []struct {
		name       string
		variants   []Variant
		provider   Provider
		diagnostic string
	}{
		{"APT after future variant", []Variant{future, {Provider: APT, Identifier: "app"}}, APT, ""},
		{"missing variant", nil, APT, `software "app" has no APT variant`},
		{"no fallback", []Variant{future}, APT, `software "app" has no APT variant`},
		{"unsupported provider", []Variant{future}, Provider("flatpak"), `unsupported provider "flatpak"`},
		{"provider not explicit", []Variant{{Provider: APT, Identifier: "app"}}, "", `unsupported provider ""`},
		{"missing variant provider", []Variant{{Identifier: "app"}}, APT, "invalid variant provider"},
		{"invalid variant provider", []Variant{{Provider: "apt cache", Identifier: "app"}}, APT, "invalid variant provider"},
		{"duplicate APT variants", []Variant{{Provider: APT, Identifier: "app"}, {Provider: APT, Identifier: "other"}}, APT, "duplicate variant provider"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := fixture("app")
			entry.Variants = tc.variants
			got, err := Resolve(Catalog{"app": entry}, Desired{IDs: []string{"app"}, Provider: tc.provider})
			if tc.diagnostic != "" {
				if err == nil || !strings.Contains(err.Error(), tc.diagnostic) || got != nil {
					t.Fatalf("result = %+v, error = %v; want %q and no result", got, err, tc.diagnostic)
				}
				return
			}
			if err != nil || len(got) != 1 || got[0].Variant.Provider != APT || got[0].Variant.Identifier != "app" {
				t.Fatalf("result = %+v, error = %v", got, err)
			}
		})
	}
}

func TestResolveAPTIdentifiers(t *testing.T) {
	for _, identifier := range []string{"", " ", "git bash", "--help", "Git", "git\n", "git;bash", "git:*", "git/other"} {
		t.Run(identifier, func(t *testing.T) {
			entry := fixture("app")
			entry.Variants[0].Identifier = identifier
			got, err := Resolve(Catalog{"app": entry}, Desired{IDs: []string{"app"}, Provider: APT})
			if err == nil || !strings.Contains(err.Error(), "invalid APT package identifier") || got != nil {
				t.Fatalf("result = %+v, error = %v; want invalid APT package identifier", got, err)
			}
		})
	}
	for _, identifier := range []string{"git", "libstdc++6", "python3.11", "libc6:amd64"} {
		entry := fixture("app")
		entry.Variants[0].Identifier = identifier
		got, err := Resolve(Catalog{"app": entry}, Desired{IDs: []string{"app"}, Provider: APT})
		if err != nil || len(got) != 1 || got[0].Variant.Identifier != identifier {
			t.Fatalf("identifier %q: result = %+v, error = %v", identifier, got, err)
		}
	}
}

func TestResolveDeduplicatesLogicalIDsNotPackages(t *testing.T) {
	first, second := fixture("first"), fixture("second")
	first.Variants[0].Identifier = "shared"
	second.Variants[0].Identifier = "shared"
	got, err := Resolve(Catalog{"first": first, "second": second}, Desired{IDs: []string{"first", "second", "first"}, Provider: APT})
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, got, []string{"first", "second"})
}

func TestResolveDependencyVariantAndEmptySelection(t *testing.T) {
	dependency := fixture("dep")
	dependency.Variants = nil
	got, err := Resolve(Catalog{"app": fixture("app", "dep"), "dep": dependency}, Desired{IDs: []string{"app"}, Provider: APT})
	if err == nil || !strings.Contains(err.Error(), `software "app" requires "dep": software "dep" has no APT variant`) || got != nil {
		t.Fatalf("result = %+v, error = %v", got, err)
	}
	got, err = Resolve(nil, Desired{Provider: APT})
	if err != nil || len(got) != 0 {
		t.Fatalf("empty selection: result = %+v, error = %v", got, err)
	}
	_, err = Resolve(nil, Desired{Provider: Provider("flatpak")})
	if err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("unsupported provider accepted for empty selection: %v", err)
	}
}

func assertIDs(t *testing.T, got []Resolved, want []string) {
	t.Helper()
	ids := make([]string, len(got))
	for i, item := range got {
		ids[i] = item.Software.ID
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("resolved IDs = %v; want %v", ids, want)
	}
}
