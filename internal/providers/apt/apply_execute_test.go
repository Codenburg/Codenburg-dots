//go:build linux

package apt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// NewApplyProviderFixture exists only in the test binary so external workflow
// tests can inject commands/checks without a shipped privilege/environment bypass.
func NewApplyProviderFixture(check func() error, command func(context.Context, string, ...string) *exec.Cmd) *Provider {
	return newApplyProvider(check, command)
}

func TestApplyProviderNativeProfilesBeforeInitialState(t *testing.T) {
	for _, tc := range []struct{ name, tool, text string }{
		{name: "force all", tool: "dpkg", text: "force-all\n"},
		{name: "native hook", tool: "dpkg", text: "pre-invoke=private-hook\n"},
		{name: "query redirect", tool: "dpkg-query", text: "admindir=/alternate\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := dpkgFixtureFiles("")
			files["etc/dpkg/"+tc.tool+".cfg"] = dpkgFixtureFiles(tc.text)["etc/dpkg/dpkg.cfg"]
			checks, commands := 0, 0
			p := newApplyProvider(func() error { checks++; return checkNativeDpkgConfig(dpkgFixtureFS(files)) },
				func(context.Context, string, ...string) *exec.Cmd { commands++; return nil })
			if checks != 0 || commands != 0 {
				t.Fatal("apply constructor queried state")
			}
			if _, err := p.InspectApplyState(context.Background(), "demo:amd64"); err == nil || commands != 0 {
				t.Fatalf("unsafe system profile queried state: %v", err)
			}
		})
	}
}

func TestExecutorReusesApplyBackendWithoutDoubleWrapping(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "dpkg")
	e, runner, _ := fixtureExecutor(t, "apt-good", marker, io.Discard, io.Discard)
	checks := 0
	e.provider = newApplyProvider(func() error { checks++; return nil },
		func(ctx context.Context, path string, args ...string) *exec.Cmd {
			out, stderr, err := (executionFixtureRunner{runner: runner}).Run(ctx, path, args...)
			code := 0
			if err != nil {
				var exit interface{ ExitCode() int }
				if !errors.As(err, &exit) {
					t.Fatal(err)
				}
				code = exit.ExitCode()
			}
			return exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAPTExecutionHelper$", "--", "metadata-output", out, stderr, strconv.Itoa(code))
		})
	if checks != 0 {
		t.Fatal("factory was not lazy")
	}
	if err := e.Execute(context.Background(), hookInstallPreview()); err != nil {
		t.Fatal("production apply backend was double-wrapped or bypassed", err)
	}
	if checks < 2*len(runner.calls) {
		t.Fatal("executor lost authoritative query guards")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("secured fixture did not reach fake dpkg", err)
	}
}

func TestExecutorLazyPrivilegeBoundary(t *testing.T) {
	r := hookRunner()
	e := NewExecutor(New(r), strings.NewReader(""), io.Discard, io.Discard)
	called := false
	e.uid = func() int { return 1000 }
	e.command = func(ctx context.Context, name string, args ...string) *exec.Cmd { called = true; return nil }
	if len(r.calls) != 0 {
		t.Fatal("constructor queried state")
	}
	if err := e.Execute(context.Background(), hookInstallPreview()); err == nil {
		t.Fatal("nonroot execute accepted")
	}
	if called || len(r.calls) != 0 {
		t.Fatal("privilege failure invoked subprocess")
	}
}

func fixtureExecutor(t *testing.T, mode, marker string, stdout, stderr io.Writer) (*Executor, *fakeRunner, *[]string) {
	t.Helper()
	r := hookRunner()
	r.results["apt-get --version"] = result{out: "apt 2.6.1 (amd64)\n"}
	r.results["apt-config dump"] = result{out: "Dir \"/\";\nDPkg::Pre-Install-Pkgs:: \"" + preconfigureHook + "\";\n"}
	e := NewExecutor(New(executionFixtureRunner{r}), strings.NewReader(""), stdout, stderr)
	e.uid = func() int { return 0 } // Private injection; never reachable in shipped CLI.
	e.checkHost = func() error { return nil }
	var argv []string
	e.command = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		argv = append([]string{name}, args...)
		return exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAPTExecutionHelper$", "--", mode, args[1], marker)
	}
	return e, r, &argv
}

type executionFixtureRunner struct{ runner *fakeRunner }

func (r executionFixtureRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	if !strings.HasPrefix(name, "/usr/bin/") {
		return "", "", errors.New("metadata executable not absolute")
	}
	return r.runner.Run(ctx, strings.TrimPrefix(name, "/usr/bin/"), args...)
}

func TestExecutorInjectedNativeBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, mode       string
		success, mutated bool
	}{
		{"exact guarded install", "apt-good", true, true},
		{"exact approved ordinary removal", "apt-removal", true, true},
		{"guard version drift before dpkg", "apt-drift", false, false},
		{"purge rejected before dpkg", "apt-purge", false, false},
		{"zero exit without hook", "apt-nohook", false, false},
		{"second hook rejected before dpkg", "apt-replay", false, false},
		{"unsupported protocol", "apt-v2", false, false},
		{"missing frontend lock", "apt-nolock", false, false},
		{"changed effective guard order", "apt-order", false, false},
		{"missing effective guard setting", "apt-incomplete", false, false},
		{"native PTY profile true", "apt-pty-true", false, false},
		{"native PTY profile missing", "apt-pty-missing", false, false},
		{"native PTY profile conflicting", "apt-pty-conflict", false, false},
		{"stderr exit failure", "apt-exit", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "dpkg")
			var out, stderr bytes.Buffer
			e, _, argv := fixtureExecutor(t, tc.mode, marker, &out, &stderr)
			preview := hookInstallPreview()
			if tc.mode == "apt-removal" {
				preview.Operations = append(preview.Operations, hookRemoval(false, false))
			}
			err := e.Execute(context.Background(), preview)
			if (err == nil) != tc.success {
				t.Fatalf("success=%v err=%v output=%s", tc.success, err, stderr.String())
			}
			_, statErr := os.Stat(marker)
			if (statErr == nil) != tc.mutated {
				t.Fatalf("guard-before-dpkg marker mismatch: %v", statErr)
			}
			if len(*argv) != 6 || (*argv)[0] != "/usr/bin/apt-get" || (*argv)[1] != "-c" || (*argv)[3] != "install" || (*argv)[4] != "--" || (*argv)[5] != "demo:amd64=2" {
				t.Fatalf("unexpected native argv: %q", *argv)
			}
			if !strings.HasPrefix((*argv)[2], "/tmp/cdots-") {
				t.Fatal("unsafe config path")
			}
			if _, err := os.Stat(filepath.Dir((*argv)[2])); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("channel leaked: %v", err)
			}
			if tc.mode == "apt-good" && (out.String() != "native output\n" || !strings.Contains(stderr.String(), "native diagnostic")) {
				t.Fatal("native output not streamed")
			}
			if tc.mode == "apt-exit" {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 23 || !strings.Contains(err.Error(), "native decline") {
					t.Fatalf("exit/diagnostic lost: %v", err)
				}
			}
		})
	}
}

func TestExecutorRefreshesHostProfileBeforeFakeDpkg(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "dpkg")
	e, _, _ := fixtureExecutor(t, "apt-good", marker, io.Discard, io.Discard)
	checks := 0
	e.checkHost = func() error {
		checks++
		if checks > 1 {
			return errors.New("unsupported native dpkg configuration drift")
		}
		return nil
	}
	if err := e.Execute(context.Background(), hookInstallPreview()); err == nil {
		t.Fatal("native configuration drift accepted")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("native configuration drift reached fake dpkg")
	}
}

func TestExecutorNativeConfigurationGate(t *testing.T) {
	for _, tc := range []struct {
		name, initial, fresh string
		launched, query      bool
	}{
		{name: "force before apt", initial: "force-all\n"},
		{name: "hook before apt", initial: "pre-invoke=private-hook\n"},
		{name: "query redirect before apt", initial: "admindir=/alternate\n", query: true},
		{name: "native drift after approval", initial: "no-debsig\nlog /var/log/dpkg.log\n", fresh: "force-confnew\n", launched: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "dpkg")
			e, _, argv := fixtureExecutor(t, "apt-good", marker, io.Discard, io.Discard)
			files := dpkgFixtureFiles(tc.initial)
			if tc.query {
				files["etc/dpkg/dpkg-query.cfg"] = files["etc/dpkg/dpkg.cfg"]
				files["etc/dpkg/dpkg.cfg"] = dpkgFixtureFiles("")["etc/dpkg/dpkg.cfg"]
			}
			checks := 0
			e.checkHost = func() error {
				checks++
				if checks > 1 {
					files["etc/dpkg/dpkg.cfg"].Data = []byte(tc.fresh)
				}
				return checkNativeDpkgConfig(dpkgFixtureFS(files))
			}
			if err := e.Execute(context.Background(), hookInstallPreview()); err == nil {
				t.Fatal("unsafe native configuration accepted")
			}
			if (len(*argv) != 0) != tc.launched {
				t.Fatal("initial native configuration gate did not precede apt start")
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unsafe native configuration reached fake dpkg")
			}
		})
	}
}

func TestExecutorQueryConfigurationDrift(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "dpkg")
	e, _, _ := fixtureExecutor(t, "apt-good", marker, io.Discard, io.Discard)
	files := dpkgFixtureFiles("no-debsig\nlog /var/log/dpkg.log\n")
	checks := 0
	e.checkHost = func() error {
		checks++
		if checks > 1 {
			files["etc/dpkg/dpkg-query.cfg"] = dpkgFixtureFiles("admindir=/alternate\n")["etc/dpkg/dpkg.cfg"]
		}
		return checkNativeDpkgConfig(dpkgFixtureFS(files))
	}
	if err := e.Execute(context.Background(), hookInstallPreview()); err == nil {
		t.Fatal("native query configuration drift accepted")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("redirected query evidence reached fake dpkg")
	}
}

func TestExecutionMetadataEnvironmentIsClean(t *testing.T) {
	for _, key := range []string{"HOME", "DPKG_FORCE", "DPKG_ROOT", "DPKG_ADMINDIR", "APT_CONFIG", "LD_PRELOAD", "DEBIAN_FRONTEND", "LC_CTYPE"} {
		t.Setenv(key, "fixture-only-untrusted")
	}
	for _, runner := range []Runner{ExecRunner{}, &ExecRunner{}} {
		for _, name := range []string{"dpkg-query", "dpkg", "apt-config", "apt-cache", "apt-get"} {
			t.Run(fmt.Sprintf("%T/%s", runner, name), func(t *testing.T) {
				called := false
				r := executionRunner{runner: runner, command: func(ctx context.Context, path string, args ...string) *exec.Cmd {
					called = true
					if path != "/usr/bin/"+name {
						t.Fatal("metadata path not rooted", path)
					}
					return exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAPTExecutionHelper$", "--", "apt-env-only", "unused", "unused")
				}}
				if _, _, err := r.Run(context.Background(), name, "fixture-only"); err != nil || !called {
					t.Fatalf("clean native metadata environment failed: %v", err)
				}
			})
		}
	}
}

func TestExecutorActualChildEnvironmentIsClean(t *testing.T) {
	// Supplied options.c: HOME must be ABSENT, not merely empty, to prevent
	// implicit ~/.dpkg.cfg loading. The actual fake apt child asserts an exact
	// environment allowlist before invoking its source-modeled native helper.
	for _, key := range []string{
		"HOME", "APT_CONFIG", "DPKG_FORCE", "DPKG_ROOT", "DPKG_ADMINDIR",
		"DPKG_FRONTEND_LOCKED", "APT_HOOK_INFO_FD", "DPKG_COLORS", "DPKG_NLS",
		"LD_PRELOAD", "LD_LIBRARY_PATH", "GCONV_PATH", "GLIBC_TUNABLES",
		"DEBIAN_FRONTEND", "DEBCONF_NONINTERACTIVE_SEEN", "LANG", "LC_CTYPE",
	} {
		t.Setenv(key, "fixture-only-untrusted")
	}
	marker := filepath.Join(t.TempDir(), "dpkg")
	e, _, _ := fixtureExecutor(t, "apt-good", marker, io.Discard, io.Discard)
	if err := e.Execute(context.Background(), hookInstallPreview()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("clean actual native child did not reach fake dpkg", err)
	}
}

func TestExecutorCancellation(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "dpkg")
	e, _, argv := fixtureExecutor(t, "apt-timeout", marker, io.Discard, io.Discard)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := e.Execute(ctx, hookInstallPreview())
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 3*time.Second {
		t.Fatalf("deadline lost or process stuck: %v", err)
	}
	if len(*argv) > 2 {
		if _, err := os.Stat(filepath.Dir((*argv)[2])); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("cancelled channel leaked")
		}
	}
	cancelled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	*argv = nil
	if err = e.Execute(cancelled, hookInstallPreview()); !errors.Is(err, context.Canceled) || len(*argv) != 0 {
		t.Fatal("prelaunch cancellation ignored")
	}
}

type dpkgReadyOutput struct {
	mu    sync.Mutex
	data  strings.Builder
	ready chan struct{}
	once  sync.Once
}

func (w *dpkgReadyOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.data.Write(p)
	if strings.Contains(w.data.String(), "no-pty dpkg ready") {
		w.once.Do(func() { close(w.ready) })
	}
	return len(p), nil
}

func TestExecutorNoPTYNativeChildCancellation(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "dpkg-pid")
	output := &dpkgReadyOutput{ready: make(chan struct{})}
	e, _, argv := fixtureExecutor(t, "apt-no-pty-cancel", marker, output, io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- e.Execute(ctx, hookInstallPreview()) }()
	select {
	case <-output.ready:
	case err := <-finished:
		t.Fatalf("source-modeled no-PTY child not reached: %v", err)
	case <-time.After(5 * time.Second):
		cancel()
		<-finished
		t.Fatal("source-modeled no-PTY child stalled")
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	assertExpiredProcess(t, pid)
	if _, err := os.Stat(filepath.Dir((*argv)[2])); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("no-PTY invocation leaked IPC", err)
	}
}

func TestExecutorConfigRejectsBeforeInvocation(t *testing.T) {
	for _, tc := range []struct{ name, dump string }{
		{"preinvoke", "DPkg::Pre-Invoke:: \"custom\";\n"},
		{"postinvoke", "DPkg::Post-Invoke:: \"custom\";\n"},
		{"custom preinstall", "DPkg::Pre-Install-Pkgs:: \"custom\";\n"},
		{"apt-get binary hooks", "Binary::apt-get::DPkg::Pre-Invoke:: \"custom\";\n"},
		{"purge", "APT::Get::Purge \"true\";\n"},
		{"dpkg override", "DPkg::Options:: \"--force-all\";\n"},
		{"alternate db", "Dir::State::status \"/tmp/status\";\n"},
		{"chroot", "DPkg::Chroot-Directory \"/tmp/root\";\n"},
		{"no lock", "Debug::NoLocking \"true\";\n"},
		{"unattended", "APT::Get::Assume-Yes \"true\";\n"},
		{"duplicate default", "DPkg::Pre-Install-Pkgs:: \"" + preconfigureHook + "\";\nDPkg::Pre-Install-Pkgs:: \"" + preconfigureHook + "\";\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, r, argv := fixtureExecutor(t, "apt-good", "", io.Discard, io.Discard)
			r.results["apt-config dump"] = result{out: tc.dump}
			if err := e.Execute(context.Background(), hookInstallPreview()); err == nil || len(*argv) != 0 {
				t.Fatalf("unsafe config launched process: %v", err)
			}
		})
	}
}

func TestExecutorCriticalRemovalRejectedBeforeLaunch(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		essential, protected bool
	}{
		{name: "essential", essential: true}, {name: "protected", protected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _, argv := fixtureExecutor(t, "apt-good", "", io.Discard, io.Discard)
			v := hookInstallPreview()
			v.Operations = append(v.Operations, hookRemoval(tc.essential, tc.protected))
			if err := e.Execute(context.Background(), v); err == nil || len(*argv) != 0 {
				t.Fatalf("critical removal launched: %v", err)
			}
		})
	}
}

func TestExecutorProductionHostGate(t *testing.T) {
	t.Setenv("DPKG_ROOT", "/unsupported-database")
	e, r, argv := fixtureExecutor(t, "apt-good", "", io.Discard, io.Discard)
	e.checkHost = checkExecutionHost // Restore the actual shipped checker.
	err := e.Execute(context.Background(), hookInstallPreview())
	if err == nil || !strings.Contains(err.Error(), "configuration redirect") || len(*argv) != 0 || len(r.calls) != 0 {
		t.Fatalf("unsupported host configuration launched work: %v", err)
	}
}

func TestExecutorSupportedNativePTYOverride(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "dpkg")
	e, r, _ := fixtureExecutor(t, "apt-good", marker, io.Discard, io.Discard)
	r.results["apt-config dump"] = result{out: "Dpkg::Use-Pty \"true\";\nDPkg::Pre-Install-Pkgs:: \"" + preconfigureHook + "\";\n"}
	if err := e.Execute(context.Background(), hookInstallPreview()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("source-supported no-PTY override failed", err)
	}
}

func TestExecutorSourceVersionRequired(t *testing.T) {
	e, r, argv := fixtureExecutor(t, "apt-good", "", io.Discard, io.Discard)
	r.results["apt-get --version"] = result{out: "apt 99.0 (amd64)\n"}
	if err := e.Execute(context.Background(), hookInstallPreview()); err == nil || len(*argv) != 0 {
		t.Fatalf("unsupported native version launched: %v", err)
	}
}

func TestExecutionPinsOnlyRequestedInstalls(t *testing.T) {
	v := hookInstallPreview()
	v.Operations = append(v.Operations, upgradeOperation())
	got, err := executionOperands(v)
	if err != nil || strings.Join(got, " ") != "demo:amd64=2" {
		t.Fatalf("dependency manually pinned: %v %v", got, err)
	}
	for i := range v.Operations {
		if v.Operations[i].Package == "demo" {
			v.Operations[i].Architecture = "all"
		}
	}
	got, err = executionOperands(v)
	if err != nil || strings.Join(got, " ") != "demo=2" {
		t.Fatalf("architecture all qualifier: %v %v", got, err)
	}
}

// TestAPTExecutionHelper is an actual subprocess fixture. Fake apt launches the
// native-style helper, waits for its exit, and only THEN reaches fake dpkg. There
// is no real apt-get/dpkg mutation invocation anywhere in these tests.
func TestAPTExecutionHelper(t *testing.T) {
	i := 0
	for i < len(os.Args) && os.Args[i] != "--" {
		i++
	}
	if i == len(os.Args) {
		return
	}
	args := os.Args[i+1:]
	if len(args) < 3 {
		os.Exit(90)
	}
	mode, config, marker := args[0], args[1], args[2]
	if mode == "metadata-output" {
		if len(args) != 4 {
			os.Exit(107)
		}
		code, err := strconv.Atoi(args[3])
		if err != nil {
			os.Exit(108)
		}
		fmt.Fprint(os.Stdout, config)
		fmt.Fprint(os.Stderr, marker)
		os.Exit(code)
	}
	if strings.HasPrefix(mode, "apt-") {
		for _, value := range os.Environ() {
			key, _, _ := strings.Cut(value, "=")
			if key != "PATH" && key != "LC_ALL" && key != "TERM" {
				os.Exit(105)
			}
		}
		if os.Getenv("PATH") != "/usr/sbin:/usr/bin:/sbin:/bin" || os.Getenv("LC_ALL") != "C" {
			os.Exit(106)
		}
	}
	if mode == "guard" {
		if os.Getenv("APT_HOOK_INFO_FD") != "0" || os.Getenv("DPKG_FRONTEND_LOCKED") != "true" {
			os.Exit(91)
		}
		if err := guardClient(config, os.Stdin); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(92)
		}
		os.Exit(0)
	}
	if mode == "dpkg-no-pty" {
		// Source-modeled ExecFork + SetupSlavePtyMagic(false): no setsid,
		// no setpgid and inherited native frontend group. Ignore SIGINT as
		// dpkg's script wait may do; context cancellation must kill the group.
		expected, err := strconv.Atoi(config)
		if err != nil || syscall.Getpgrp() != expected {
			os.Exit(101)
		}
		signal.Ignore(syscall.SIGINT)
		if err := os.WriteFile(marker, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			os.Exit(102)
		}
		fmt.Fprintln(os.Stdout, "no-pty dpkg ready")
		time.Sleep(20 * time.Second)
		os.Exit(0)
	}
	if mode == "apt-env-only" {
		os.Exit(0)
	}
	if mode == "apt-timeout" {
		time.Sleep(20 * time.Second)
		return
	}
	if mode == "apt-nohook" {
		return
	}
	if mode == "apt-exit" {
		fmt.Fprintln(os.Stderr, "native decline")
		os.Exit(23)
	}
	data, err := os.ReadFile(config)
	if err != nil {
		os.Exit(93)
	}
	var protocol strings.Builder
	protocol.WriteString("VERSION 3\nBinary=apt-get\n")
	hooks := []string{}
	noPTY := false
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if strings.HasPrefix(line, "#clear ") {
			continue
		}
		settings, err := parseExecutionDump(line + "\n")
		if err != nil {
			os.Exit(94)
		}
		for key, values := range settings {
			for _, value := range values {
				if key == "dpkg::use-pty" {
					noPTY = value == "false"
					switch mode {
					case "apt-pty-missing":
						continue
					case "apt-pty-true":
						value = "true"
					case "apt-pty-conflict":
						protocol.WriteString("dpkg::use-pty=true\n")
					}
				}
				if key == "dpkg::pre-install-pkgs::" {
					hooks = append(hooks, value)
				}
				if mode == "apt-purge" && key == "apt::get::purge" {
					value = "true"
				}
				if mode == "apt-order" && key == "dpkg::pre-install-pkgs::" {
					value = "wrong hook"
				}
				if mode == "apt-incomplete" && key == "debug::nolocking" {
					continue
				}
				protocol.WriteString(encodeConfig(key) + "=" + encodeConfig(value) + "\n")
			}
		}
	}
	if len(hooks) == 0 {
		os.Exit(95)
	}
	fields := strings.Fields(hooks[0])
	if len(fields) != 3 || fields[1] != "--internal-apt-guard" || !validGuardPath(fields[2]) {
		os.Exit(96)
	}
	rows := hookInstallRows
	if mode == "apt-removal" {
		rows += "libdemo 1 amd64 foreign > - - none **REMOVE**\n"
	}
	if mode == "apt-drift" {
		rows = strings.ReplaceAll(rows, "< 2", "< 3")
	}
	protocol.WriteString("\n" + rows)
	payload := protocol.String()
	if mode == "apt-v2" {
		payload = strings.Replace(payload, "VERSION 3", "VERSION 2", 1)
	}
	call := func() error {
		cmd := exec.Command(os.Args[0], "-test.run=^TestAPTExecutionHelper$", "--", "guard", fields[2], marker)
		cmd.Stdin = strings.NewReader(payload)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = append(os.Environ(), "APT_HOOK_INFO_FD=0")
		if mode != "apt-nolock" {
			cmd.Env = append(cmd.Env, "DPKG_FRONTEND_LOCKED=true")
		}
		return cmd.Run()
	}
	if err := call(); err != nil {
		os.Exit(97)
	}
	if mode == "apt-replay" {
		if err := call(); err != nil {
			os.Exit(98)
		}
	}
	if mode == "apt-no-pty-cancel" {
		if !noPTY {
			os.Exit(103)
		}
		// Tagged dpkgpm.cc:2008 calls ExecFork then SetupSlavePtyMagic.
		// Tagged fileutl.cc:898 preserves the group; Use-Pty=false returns
		// before dpkgpm.cc:1309's setsid. This mock deliberately does likewise.
		cmd := exec.Command(os.Args[0], "-test.run=^TestAPTExecutionHelper$", "--", "dpkg-no-pty", strconv.Itoa(syscall.Getpgrp()), marker)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			os.Exit(104)
		}
		os.Exit(0)
	}
	fmt.Fprintln(os.Stdout, "native output")
	fmt.Fprintln(os.Stderr, "native diagnostic")
	if marker != "" {
		if err := os.WriteFile(marker, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			os.Exit(99)
		}
	}
	os.Exit(0)
}
