//go:build linux

package apt

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const debian12ProfileDir = "testdata/debian12-apt-2.6.1"

func TestDebian12ConfigFragmentDirectives(t *testing.T) {
	for _, name := range []string{"00CDMountPoint", "00trustcdrom", "01autoremove", "20listchanges", "70debconf"} {
		t.Run(name, func(t *testing.T) {
			if err := checkConfigIncludes(filepath.Join(debian12ProfileDir, name)); err != nil {
				t.Fatal(err)
			}
		})
	}

	for _, tc := range []struct {
		name, contents string
		accept         bool
	}{
		{"ordinary comments", "  # documentation\nAPT::Example \"value\";\n\t# another comment\n", true},
		{"clear directive", "#clear APT::Example;\n", false},
		{"include directive", "  #include \"fixture.conf\";\n", false},
		{"configure index directive", "#x-apt-configure-index \"fixture.conf\";\n", false},
		{"inline hash", "APT::Example \"value\"; # comment\n", false},
		{"ambiguous percent", "APT::Example%20 \"value\";\n", false},
		{"unsupported block comment", "/* documentation\n */\nAPT::Example \"value\";\n", false},
		{"inert block marker in hash comment", "# documentation with /* a marker\nAPT::Example \"value\";\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "90test")
			if err := os.WriteFile(path, []byte(tc.contents), 0600); err != nil {
				t.Fatal(err)
			}
			err := checkConfigIncludes(path)
			if tc.accept && err != nil {
				t.Fatal(err)
			}
			if !tc.accept && (err == nil || err.Error() != "APT configuration directives/includes unsupported") {
				t.Fatalf("expected directive rejection, got %v", err)
			}
		})
	}
}

func TestDebian12FullDumpRecognizesLiteralRegexButRejectsUnsupportedExecution(t *testing.T) {
	dump, err := os.ReadFile(filepath.Join(debian12ProfileDir, "apt-config-dump.txt"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := parseExecutionDump(string(dump))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"~$", `\.disabled$`, `\.bak$`, `\.dpkg-[a-z]+$`, `\.ucf-[a-z]+$`, `\.save$`, `\.orig$`, `\.distUpgrade$`}
	if got := cfg["dir::ignore-files-silently::"]; !slices.Equal(got, want) {
		t.Fatalf("ignore-file list changed: %q", got)
	}
	if hooks, err := checkExecutionConfig(cfg, false); hooks != nil || err == nil {
		t.Fatalf("expected unsupported execution profile to fail closed, got %v", err)
	}
}

func TestDebian12DumpRejectsAmbiguousEncoding(t *testing.T) {
	for _, line := range []string{
		`Dir::Ignore-Files-Silently:: "\.custom$";`,
		`Dir::Bin::dpkg "\.bak$";`,
		`Dir::Ignore-Files-Silently%20:: "value";`,
	} {
		if cfg, err := parseExecutionDump(line + "\n"); cfg != nil || err == nil {
			t.Fatalf("expected ambiguous dump to be rejected: %s", line)
		}
	}
}

func TestPinnedDumpRejectsNoncanonicalKeyBytes(t *testing.T) {
	for _, key := range []string{"APT::Examplé", "APT::Example\v"} {
		if cfg, err := parseExecutionDumpFormat(key+" \"value\";\n", true); cfg != nil ||
			err == nil || err.Error() != "unsupported APT configuration syntax" {
			t.Fatalf("key %q: expected syntax rejection, got %v", key, err)
		}
	}
}

// The captured VM dump uses APT's default raw-value formatter.
func parseExecutionDump(dump string) (executionSettings, error) {
	return parseExecutionDumpFormat(dump, false)
}
