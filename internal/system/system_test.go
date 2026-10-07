package system

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseOSReleaseHandlesQuotedEscapes(t *testing.T) {
	got, err := ParseOSRelease(strings.NewReader("NAME=Linux\\ Mint\nID=linuxmint\nID_LIKE=ubuntu debian\nPRETTY_NAME=\"Mint \\\"M\\\"\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got["PRETTY_NAME"] != `Mint "M"` || got["ID"] != "linuxmint" {
		t.Fatalf("unexpected parsed values: %#v", got)
	}
}

func TestParseOSReleaseRejectsMalformedInput(t *testing.T) {
	for _, input := range []string{"ID=ubuntu\nBROKEN", "ID='unclosed", "ID=ubuntu\nID=debian"} {
		if _, err := ParseOSRelease(strings.NewReader(input)); err == nil {
			t.Errorf("ParseOSRelease(%q) accepted malformed input", input)
		}
	}
}

func TestDetectSupportUsesIDNotIDLike(t *testing.T) {
	for _, tc := range []struct {
		id        string
		supported bool
	}{{"debian", true}, {"ubuntu", true}, {"linuxmint", true}, {"pop", false}} {
		if got := IsSupportedID(tc.id); got != tc.supported {
			t.Errorf("IsSupportedID(%q) = %v", tc.id, got)
		}
	}
}

func TestRegressionOSReleaseEscaping(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`'literal\'`, `literal\`},
		{`'one\\two'`, `one\\two`},
		{`"one\qtwo"`, `one\qtwo`},
		{`"\$ \` + "`" + ` \" \\"`, "$ ` \" \\"},
		{`unquoted\q\$\"`, `unquotedq$"`},
		{`"$(touch never-run) ${HOME}"`, `$(touch never-run) ${HOME}`},
	} {
		got, err := ParseOSRelease(strings.NewReader("NAME=" + tc.raw + "\n"))
		if err != nil || got["NAME"] != tc.want {
			t.Errorf("parse %q = %q, %v; want %q", tc.raw, got["NAME"], err, tc.want)
		}
	}
}

func TestRegressionMachineArchitecture(t *testing.T) {
	for raw, want := range map[string]string{"x86_64": "amd64", "aarch64": "arm64", "riscv64": "riscv64"} {
		called := false
		run := func(ctx context.Context, command string, args ...string) (string, error) {
			called = true
			if command != "uname" || len(args) != 1 || args[0] != "-m" {
				t.Fatalf("unexpected machine query: %s %v", command, args)
			}
			if _, bounded := ctx.Deadline(); !bounded {
				t.Fatal("machine query has no timeout")
			}
			return raw + "\n", nil
		}
		arch, original, err := machineArchitecture(context.Background(), run)
		if !called || err != nil || arch != want || original != raw {
			t.Fatalf("machine architecture = %q, %q, %v; want %q, %q", arch, original, err, want, raw)
		}
	}
	failure := errors.New("uname unavailable")
	if _, _, err := machineArchitecture(context.Background(), func(context.Context, string, ...string) (string, error) { return "", failure }); !errors.Is(err, failure) {
		t.Fatalf("lost machine query error: %v", err)
	}
	for _, output := range []string{"", "x86_64\narm64\n"} {
		if _, _, err := machineArchitecture(context.Background(), func(context.Context, string, ...string) (string, error) { return output, nil }); err == nil {
			t.Errorf("invalid machine output accepted: %q", output)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := machineArchitecture(ctx, func(ctx context.Context, _ string, _ ...string) (string, error) { return "", ctx.Err() }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation hidden: %v", err)
	}
}

func TestNormalizeArchitecture(t *testing.T) {
	for raw, want := range map[string]string{"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64", "mips": "mips"} {
		got, retained := NormalizeArchitecture(raw)
		if got != want || retained != raw {
			t.Errorf("NormalizeArchitecture(%q) = %q, %q", raw, got, retained)
		}
	}
}
