package apt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func hookInstallPreview() Preview {
	v := installPreview()
	op := v.Operations[0]
	op.Kind = "configuration"
	v.Operations = append(v.Operations, op)
	return v
}

func hookRunner() *fakeRunner {
	return &fakeRunner{results: map[string]result{
		"dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- demo:amd64": {
			err: commandError(1), stderr: "dpkg-query: no packages found matching demo:amd64"},
		"dpkg-query -W -f=${Status}\\t${Version}\\t${Architecture}\\n -- demo:amd64": {
			err: commandError(1), stderr: "dpkg-query: no packages found matching demo:amd64"},
		"apt-cache policy demo:amd64":    {out: "Candidate: 2\n"},
		"apt-cache show -- demo:amd64=2": {out: "Package: demo\nArchitecture: amd64\nVersion: 2\n"},
		"dpkg-query --status -- libdemo:amd64": {
			out: "Package: libdemo\nArchitecture: amd64\nVersion: 1\nStatus: install ok installed\n"},
		"apt-cache show -- libdemo:amd64=2": {out: "Package: libdemo\nArchitecture: amd64\nVersion: 2\n"},
		"dpkg --compare-versions 2 gt 1":    {},
		"dpkg --compare-versions 2 eq 1":    {err: commandError(1)},
		"dpkg --compare-versions 1 eq 1":    {},
	}}
}

// Native fields follow SendPkgsInfo in official Debian apt 2.4.5/2.6.1
// apt-pkg/deb/dpkgpm.cc. Fixtures never run native APT or dpkg mutations.
const hookInstallRows = "demo - - none < 2 amd64 none /var/cache/apt/archives/demo with spaces.deb\n" +
	"demo - - none < 2 amd64 none **CONFIGURE**\n"

func assertHookReadOnly(t *testing.T, r *fakeRunner) {
	t.Helper()
	for _, call := range r.calls {
		if !strings.HasPrefix(call, "dpkg-query -W ") && !strings.HasPrefix(call, "dpkg-query --status -- ") &&
			!strings.HasPrefix(call, "apt-cache policy ") && !strings.HasPrefix(call, "apt-cache show -- ") &&
			!strings.HasPrefix(call, "dpkg --compare-versions ") {
			t.Fatalf("non-read-only command: %s", call)
		}
	}
}

func TestValidateHookExactInstall(t *testing.T) {
	for _, tc := range []struct {
		name, rows string
		good       bool
	}{
		{name: "exact install and configuration", rows: hookInstallRows, good: true},
		{name: "missing configuration", rows: strings.Split(hookInstallRows, "\n")[0] + "\n"},
		{name: "missing install", rows: strings.Split(hookInstallRows, "\n")[1] + "\n"},
		{name: "unexpected install", rows: hookInstallRows + "other - - none < 2 amd64 none /other.deb\n"},
		{name: "duplicate install", rows: hookInstallRows + strings.Split(hookInstallRows, "\n")[0] + "\n"},
		{name: "duplicate configuration", rows: hookInstallRows + strings.Split(hookInstallRows, "\n")[1] + "\n"},
		{name: "changed install version", rows: strings.Replace(hookInstallRows, "< 2", "< 3", 1)},
		{name: "changed configuration version", rows: strings.ReplaceAll(hookInstallRows, "< 2", "< 3")},
		{name: "changed new architecture", rows: strings.ReplaceAll(hookInstallRows, "amd64", "arm64")},
		{name: "present unexpected old version", rows: strings.ReplaceAll(hookInstallRows, "- - none", "1 amd64 none")},
		{name: "substituted action same count", rows: strings.Replace(hookInstallRows, "**CONFIGURE**", "/again.deb", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := hookRunner()
			err := New(r).ValidateHook(context.Background(), hookInstallPreview(), strings.NewReader("VERSION 3\n\n"+tc.rows))
			if (err == nil) != tc.good {
				t.Fatalf("good=%v err=%v", tc.good, err)
			}
			assertHookReadOnly(t, r)
		})
	}
}

func TestValidateHookChangesAndConfigurationNormalization(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   Operation
		rows string
		good bool
	}{
		{name: "upgrade with cached old configuration version", op: upgradeOperation(), good: true,
			rows: "libdemo 1 amd64 same < 2 amd64 same /libdemo.deb\nlibdemo 1 amd64 same < 2 amd64 same **CONFIGURE**\n"},
		{name: "configuration must not replace old with new", op: upgradeOperation(),
			rows: "libdemo 1 amd64 same < 2 amd64 same /libdemo.deb\nlibdemo 2 amd64 same = 2 amd64 same **CONFIGURE**\n"},
		{name: "configuration absent old is not fallback", op: upgradeOperation(),
			rows: "libdemo 1 amd64 same < 2 amd64 same /libdemo.deb\nlibdemo - - none < 2 amd64 same **CONFIGURE**\n"},
		{name: "old architecture drift", op: upgradeOperation(),
			rows: "libdemo 1 arm64 same < 2 amd64 same /libdemo.deb\nlibdemo 1 amd64 same < 2 amd64 same **CONFIGURE**\n"},
		{name: "old version drift", op: upgradeOperation(),
			rows: "libdemo 0 amd64 same < 2 amd64 same /libdemo.deb\nlibdemo 1 amd64 same < 2 amd64 same **CONFIGURE**\n"},
		{name: "wrong comparator", op: upgradeOperation(),
			rows: "libdemo 1 amd64 same = 2 amd64 same /libdemo.deb\nlibdemo 1 amd64 same < 2 amd64 same **CONFIGURE**\n"},
		{name: "reviewed ordinary removal", op: hookRemoval(false, false), good: true,
			rows: "libdemo 1 amd64 foreign > - - none **REMOVE**\n"},
		{name: "essential removal", op: hookRemoval(true, false),
			rows: "libdemo 1 amd64 foreign > - - none **REMOVE**\n"},
		{name: "protected removal", op: hookRemoval(false, true),
			rows: "libdemo 1 amd64 foreign > - - none **REMOVE**\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := hookInstallPreview()
			v.Operations = append(v.Operations, tc.op)
			if tc.op.Kind == "upgrade" {
				conf := tc.op
				conf.Kind, conf.OldVersion = "configuration", ""
				v.Operations = append(v.Operations, conf)
			}
			r := hookRunner()
			// Reverse native row order to establish multiset, not ordering,
			// identity. The executor must separately secure hook ordering.
			err := New(r).ValidateHook(context.Background(), v, strings.NewReader("VERSION 3\n\n"+tc.rows+hookInstallRows))
			if (err == nil) != tc.good {
				t.Fatalf("good=%v err=%v", tc.good, err)
			}
			assertHookReadOnly(t, r)
		})
	}
	t.Run("standalone configuration uses installed version", func(t *testing.T) {
		v := hookInstallPreview()
		op := hookRemoval(false, false)
		op.Kind, op.OldVersion, op.NewVersion = "configuration", "", "1"
		v.Operations = append(v.Operations, op)
		r := hookRunner()
		err := New(r).ValidateHook(context.Background(), v, strings.NewReader(
			"VERSION 3\n\n"+hookInstallRows+"libdemo 1 amd64 no = 1 amd64 no **CONFIGURE**\n"))
		if err != nil {
			t.Fatal(err)
		}
		assertHookReadOnly(t, r)
	})
}

func hookRemoval(essential, protected bool) Operation {
	op := upgradeOperation()
	op.Kind, op.NewVersion, op.Candidate = "removal", "", PackageEvidence{}
	op.Installed.Essential, op.Installed.Protected = essential, protected
	return op
}

func TestValidateHookFreshEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		response      result
	}{
		{name: "installed disappeared", command: "dpkg-query --status -- libdemo:amd64", response: result{err: commandError(1)}},
		{name: "installed failed", command: "dpkg-query --status -- libdemo:amd64", response: result{err: errors.New("unavailable")}},
		{name: "installed protection unknown", command: "dpkg-query --status -- libdemo:amd64",
			response: result{out: "Package: libdemo\nArchitecture: amd64\nVersion: 1\nStatus: install ok installed\nProtected: maybe\n"}},
		{name: "installed protection changed", command: "dpkg-query --status -- libdemo:amd64",
			response: result{out: "Package: libdemo\nArchitecture: amd64\nVersion: 1\nStatus: install ok installed\nProtected: yes\n"}},
		{name: "installed essential changed", command: "dpkg-query --status -- libdemo:amd64",
			response: result{out: "Package: libdemo\nArchitecture: amd64\nVersion: 1\nStatus: install ok installed\nEssential: yes\n"}},
		{name: "fresh hold", command: "dpkg-query --status -- libdemo:amd64",
			response: result{out: "Package: libdemo\nArchitecture: amd64\nVersion: 1\nStatus: hold ok installed\n"}},
		{name: "installed unbound architecture", command: "dpkg-query --status -- libdemo:amd64",
			response: result{out: "Package: libdemo\nArchitecture: all\nVersion: 1\nStatus: install ok installed\n"}},
		{name: "candidate flags changed", command: "apt-cache show -- libdemo:amd64=2",
			response: result{out: "Package: libdemo\nArchitecture: amd64\nVersion: 2\nEssential: yes\n"}},
		{name: "candidate flags failed", command: "apt-cache show -- libdemo:amd64=2", response: result{err: commandError(1)}},
		{name: "candidate unknown", command: "apt-cache show -- demo:amd64=2",
			response: result{out: "Package: demo\nArchitecture: amd64\nVersion: 2\nEssential: \n"}},
		{name: "unexpected existing install", command: "dpkg-query -W -f=${db:Status-Abbrev}\\t${Version}\\n -- demo:amd64",
			response: result{out: "ii \t2\n"}},
		{name: "unexpected partial install", command: "dpkg-query -W -f=${Status}\\t${Version}\\t${Architecture}\\n -- demo:amd64",
			response: result{out: "install ok unpacked\t2\tamd64\n"}},
		{name: "candidate policy changed", command: "apt-cache policy demo:amd64", response: result{out: "Candidate: 3\n"}},
		{name: "version comparison unavailable", command: "dpkg --compare-versions 2 gt 1", response: result{err: commandError(2)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := hookInstallPreview()
			v.Operations = append(v.Operations, upgradeOperation())
			r := hookRunner()
			r.results[tc.command] = tc.response
			err := New(r).ValidateHook(context.Background(), v, strings.NewReader("VERSION 3\n\n"+hookInstallRows+
				"libdemo 1 amd64 none < 2 amd64 none /libdemo.deb\n"))
			if err == nil {
				t.Fatal("unsafe fresh evidence accepted")
			}
			assertHookReadOnly(t, r)
		})
	}
	for _, name := range []string{"approved hold", "approved downgrade", "approved unknown"} {
		t.Run(name, func(t *testing.T) {
			v := hookInstallPreview()
			op := upgradeOperation()
			switch name {
			case "approved hold":
				op.Installed.Held = true
			case "approved downgrade":
				op.NewVersion, op.Candidate.Version = "0", "0"
			case "approved unknown":
				op.Installed.Known = false
			}
			v.Operations = append(v.Operations, op)
			r := hookRunner()
			r.results["dpkg --compare-versions 0 gt 1"] = result{err: commandError(1)}
			if err := New(r).ValidateHook(context.Background(), v, strings.NewReader("VERSION 3\n\n"+hookInstallRows+
				"libdemo 1 amd64 none < 2 amd64 none /libdemo.deb\n")); err == nil {
				t.Fatal("unsafe approved evidence accepted")
			}
			assertHookReadOnly(t, r)
		})
	}
}

func TestValidateHookProtocolAndConfiguration(t *testing.T) {
	valid := "VERSION 3\n\n" + hookInstallRows
	for _, tc := range []struct {
		name, input string
		good        bool
	}{
		{name: "empty"},
		{name: "protocol 2", input: strings.Replace(valid, "VERSION 3", "VERSION 2", 1)},
		{name: "future protocol", input: strings.Replace(valid, "VERSION 3", "VERSION 4", 1)},
		{name: "truncated header", input: "VERSION 3\nAPT::Get::Assume-Yes=false\n"},
		{name: "unterminated header line", input: "VERSION 3"},
		{name: "no rows", input: "VERSION 3\n\n"},
		{name: "truncated row", input: strings.TrimSuffix(valid, "\n")},
		{name: "extra blank", input: valid + "\n"},
		{name: "error action", input: strings.Replace(valid, "**CONFIGURE**", "**ERROR**", 1)},
		{name: "unknown action", input: strings.Replace(valid, "**CONFIGURE**", "**UNPACK**", 1)},
		{name: "relative deb", input: strings.Replace(valid, "/var/cache/apt/archives/", "./", 1)},
		{name: "non-deb path", input: strings.Replace(valid, "spaces.deb", "spaces.exe", 1)},
		{name: "missing version", input: strings.Replace(valid, "2 amd64", "- amd64", 1)},
		{name: "missing architecture", input: strings.Replace(valid, "2 amd64", "2 -", 1)},
		{name: "missing field", input: strings.Replace(valid, "amd64 none", "amd64", 1)},
		{name: "extra field", input: strings.Replace(valid, "amd64 none", "amd64 none extra", 1)},
		{name: "unknown MultiArch", input: strings.Replace(valid, "amd64 none", "amd64 unknown", 1)},
		{name: "contradictory absent arch", input: strings.Replace(valid, "- - none", "- amd64 none", 1)},
		{name: "contradictory absent MultiArch", input: strings.Replace(valid, "- - none", "- - same", 1)},
		{name: "wrong comparator", input: strings.Replace(valid, "< 2", "> 2", 1)},
		{name: "CRLF", input: strings.ReplaceAll(valid, "\n", "\r\n")},
		{name: "tab separators", input: strings.Replace(valid, "demo -", "demo\t-", 1)},
		{name: "NUL", input: valid + "\x00\n"},
		{name: "oversize", input: "VERSION 3\nSecret=" + strings.Repeat("x", maxCommandOutput) + "\n\n" + hookInstallRows},
		{name: "malformed config", input: "VERSION 3\nCredential%ZZ=secret\n\n" + hookInstallRows},
		{name: "operation before config terminator", input: "VERSION 3\n" + hookInstallRows + "\n"},
		{name: "benign encoded configuration and repeated list", good: true, input: "VERSION 3\n" +
			"Acquire::https::Proxy=https://user:credential%20value@host/\n" +
			"APT::Architectures::=amd64\nAPT::Architectures::=all\n" +
			"Description=demo%20-%20-%20none%20%3C%202%20amd64%20none%20**REMOVE**\n" +
			"APT::Get::Assume-Yes=false\nquiet=1\n\n" + hookInstallRows},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := hookRunner()
			err := New(r).ValidateHook(context.Background(), hookInstallPreview(), strings.NewReader(tc.input))
			if (err == nil) != tc.good {
				t.Fatalf("good=%v err=%v", tc.good, err)
			}
			if err != nil && (strings.Contains(err.Error(), "credential") || strings.Contains(err.Error(), "secret")) {
				t.Fatal("native configuration leaked")
			}
			assertHookReadOnly(t, r)
		})
	}
	for _, config := range []string{
		"APT::Get::Assume-Yes=true", "APT::Get::Force-Yes=yes", "quiet=2", "quiet=bogus",
		"APT::Get::Ignore-Hold=1", "APT::Get::Allow-Unauthenticated=true", "APT::Get::AutomaticRemove=true",
		"APT::Get::Purge=true", "APT::Get::Allow-Downgrades=true", "APT::Get::Allow-Remove-Essential=true",
		"APT::Get::Allow-Change-Held-Packages=true", "DPkg::Options::=--force-all", "Debug::NoLocking=true",
		"APT::Get::Assume%2DYes=true", "APT::Ignore-Hold=true", "APT::Get::AllowUnauthenticated=true",
	} {
		t.Run(config, func(t *testing.T) {
			r := hookRunner()
			if err := New(r).ValidateHook(context.Background(), hookInstallPreview(), strings.NewReader(
				"VERSION 3\n"+config+"\n\n"+hookInstallRows)); err == nil {
				t.Fatal("unsafe inherited authority accepted")
			}
			if len(r.calls) != 0 {
				t.Fatal("unsafe protocol reached metadata queries")
			}
		})
	}
	for _, token := range []string{"none", "no", "same", "foreign", "allowed"} {
		t.Run("native MultiArch "+token, func(t *testing.T) {
			r := hookRunner()
			if err := New(r).ValidateHook(context.Background(), hookInstallPreview(), strings.NewReader(
				strings.ReplaceAll(valid, "amd64 none", "amd64 "+token))); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// The synthetic deadline reader emulates blocked pipe reads without a real
// descriptor, goroutine, or wall-clock wait. It cancels from inside Read.
type hookCancelReader struct {
	cancel   context.CancelFunc
	deadline time.Time
}

func (r *hookCancelReader) SetReadDeadline(t time.Time) error {
	r.deadline = t
	return nil
}
func (r *hookCancelReader) Read([]byte) (int, error) {
	r.cancel()
	return 0, io.EOF
}

type hookFaultReader struct {
	deadlineErr error
	read        func([]byte) (int, error)
}

func (r *hookFaultReader) SetReadDeadline(time.Time) error { return r.deadlineErr }
func (r *hookFaultReader) Read(b []byte) (int, error)      { return r.read(b) }

type hookTimeoutError struct{}

func (hookTimeoutError) Error() string   { return "synthetic timeout" }
func (hookTimeoutError) Timeout() bool   { return true }
func (hookTimeoutError) Temporary() bool { return true }

func TestValidateHookReaderBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reader io.Reader
	}{
		{name: "deadline unavailable", reader: &hookFaultReader{deadlineErr: errors.New("secret")}},
		{name: "no progress", reader: &hookFaultReader{read: func([]byte) (int, error) { return 0, nil }}},
		{name: "read error", reader: &hookFaultReader{read: func([]byte) (int, error) { return 0, errors.New("secret") }}},
		{name: "invalid count", reader: &hookFaultReader{read: func([]byte) (int, error) { return -1, nil }}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := hookRunner()
			err := New(r).ValidateHook(context.Background(), hookInstallPreview(), tc.reader)
			if err == nil || strings.Contains(err.Error(), "secret") || len(r.calls) != 0 {
				t.Fatalf("err=%v calls=%v", err, r.calls)
			}
		})
	}
	expired, expiredCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer expiredCancel()
	if err := New(hookRunner()).ValidateHook(expired, hookInstallPreview(), strings.NewReader("VERSION 3\n\n"+hookInstallRows)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired input: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := hookRunner()
	if err := New(r).ValidateHook(ctx, hookInstallPreview(), strings.NewReader("VERSION 3\n\n"+hookInstallRows)); !errors.Is(err, context.Canceled) || len(r.calls) != 0 {
		t.Fatalf("cancelled input: %v calls=%v", err, r.calls)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	reader := &hookCancelReader{cancel: cancel}
	if err := New(r).ValidateHook(ctx, hookInstallPreview(), reader); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel during input: %v", err)
	}
	if reader.deadline.IsZero() || len(r.calls) != 0 {
		t.Fatal("read deadline missing or cancellation reached metadata")
	}
	timeoutCtx, timeoutCancel := context.WithCancel(context.Background())
	defer timeoutCancel()
	attempts := 0
	timeoutReader := &hookFaultReader{read: func(b []byte) (int, error) {
		attempts++
		if attempts == 1 {
			return 0, hookTimeoutError{}
		}
		timeoutCancel()
		return 0, hookTimeoutError{}
	}}
	if err := New(r).ValidateHook(timeoutCtx, hookInstallPreview(), timeoutReader); !errors.Is(err, context.Canceled) || attempts != 2 {
		t.Fatalf("timeout retry/cancel: %v attempts=%d", err, attempts)
	}
	for _, reader := range []io.Reader{
		bytes.NewReader([]byte("VERSION 3\n\n" + hookInstallRows)),
		bytes.NewBufferString("VERSION 3\n\n" + hookInstallRows),
	} {
		if err := New(hookRunner()).ValidateHook(context.Background(), hookInstallPreview(), reader); err != nil {
			t.Fatal(err)
		}
	}
	if err := New(r).ValidateHook(context.Background(), hookInstallPreview(), io.LimitReader(strings.NewReader("x"), 1)); err == nil || len(r.calls) != 0 {
		t.Fatal("uninterruptible arbitrary reader accepted")
	}
}
