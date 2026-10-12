package software

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Codenburg/Codenburg-dots/internal/providers/flatpak"
)

func TestFlatpakVariantFilters(t *testing.T) {
	base := Variant{Provider: Flatpak, Identifier: "org.example.App", Remote: "flathub", Branch: "stable"}
	user, system, beta, arm := base, base, base, base
	user.Scope, system.Scope = flatpak.User, flatpak.System
	beta.Branch, arm.Arch = "beta", "aarch64"
	for _, tc := range []struct {
		name     string
		variants []Variant
		desired  Desired
		want     Variant
		message  string
	}{
		{name: "scope unspecified", variants: []Variant{base}, want: base},
		{name: "user filter", variants: []Variant{user, system}, desired: Desired{Scope: flatpak.User}, want: user},
		{name: "system filter", variants: []Variant{user, system}, desired: Desired{Scope: flatpak.System}, want: system},
		{name: "named scope", variants: []Variant{base}, desired: Desired{Scope: "installation:work"}, want: Variant{Provider: Flatpak, Identifier: base.Identifier, Remote: "flathub", Branch: "stable", Scope: "installation:work"}},
		{name: "ambiguous scopes", variants: []Variant{user, system}, message: "ambiguous"},
		{name: "branch filter", variants: []Variant{base, beta}, desired: Desired{Branch: "beta"}, want: beta},
		{name: "arch filter", variants: []Variant{arm}, desired: Desired{Arch: "aarch64"}, want: arm},
		{name: "no implicit arch conversion", variants: []Variant{arm}, desired: Desired{Arch: "amd64"}, message: "no matching"},
		{name: "no default branch", variants: []Variant{{Provider: Flatpak, Identifier: base.Identifier}}, want: Variant{Provider: Flatpak, Identifier: base.Identifier}},
		{name: "remote mismatch", variants: []Variant{base}, desired: Desired{Remote: "other"}, message: "no matching"},
		{name: "branch mismatch", variants: []Variant{base}, desired: Desired{Branch: "beta"}, message: "no matching"},
		{name: "duplicate identity", variants: []Variant{base, base}, message: "duplicate"},
		{name: "invalid ID", variants: []Variant{{Provider: Flatpak, Identifier: "--help"}}, message: "invalid flatpak application"},
		{name: "invalid catalog operand", variants: []Variant{{Provider: Flatpak, Identifier: base.Identifier, Remote: "--help"}}, message: "invalid flatpak selection"},
		{name: "invalid scope", variants: []Variant{base}, desired: Desired{Scope: "all"}, message: "invalid flatpak scope"},
		{name: "invalid arch", variants: []Variant{base}, desired: Desired{Arch: "x86_64/other"}, message: "invalid flatpak selection"},
		{name: "no native fallback", variants: []Variant{{Provider: APT, Identifier: "app"}}, message: "no matching Flatpak"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.desired
			d.IDs, d.Provider = []string{"app"}, Flatpak
			got, err := Resolve(Catalog{"app": {ID: "app", Name: "App", Variants: tc.variants}}, d)
			if tc.message != "" {
				if err == nil || !strings.Contains(err.Error(), tc.message) || got != nil {
					t.Fatalf("result %+v; error %v; want %q", got, err, tc.message)
				}
				return
			}
			if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0].Variant, tc.want) {
				t.Fatalf("result %+v; error %v; want %+v", got, err, tc.want)
			}
		})
	}
}

func TestProviderChoiceAndFilters(t *testing.T) {
	for _, tc := range []struct {
		name    string
		desired Desired
		want    string
	}{
		{name: "explicit native", desired: Desired{Provider: APT}, want: "firefox-esr"},
		{name: "explicit Flatpak", desired: Desired{Provider: Flatpak}, want: "org.mozilla.firefox"},
		{name: "no preference", desired: Desired{}},
		{name: "no unsupported provider", desired: Desired{Provider: "snap"}},
		{name: "native cannot use Flatpak filters", desired: Desired{Provider: APT, Branch: "stable"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.desired.IDs = []string{"firefox"}
			got, err := Resolve(BuiltinCatalog(), tc.desired)
			if tc.want == "" {
				if err == nil || got != nil {
					t.Fatalf("result %+v; error %v", got, err)
				}
				return
			}
			if err != nil || len(got) != 1 || got[0].Variant.Identifier != tc.want {
				t.Fatalf("result %+v; error %v", got, err)
			}
		})
	}
	if _, err := Resolve(nil, Desired{Provider: Flatpak, Branch: "--help"}); err == nil {
		t.Fatal("invalid filters accepted for empty selection")
	}
}
