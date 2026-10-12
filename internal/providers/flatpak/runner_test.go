//go:build linux

package flatpak

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDetection(t *testing.T) {
	for _, tc := range []struct {
		name, goos, missing, home, data string
		wantError                       bool
	}{
		{name: "supported", goos: "linux", home: "/home/test"},
		{name: "missing flatpak", goos: "linux", missing: "flatpak", home: "/home/test", wantError: true},
		{name: "missing bwrap", goos: "linux", missing: "bwrap", home: "/home/test", wantError: true},
		{name: "unsupported", goos: "darwin", home: "/home/test", wantError: true},
		{name: "relative home", goos: "linux", home: "relative", wantError: true},
		{name: "hidden data", goos: "linux", home: "/home/test", data: "/run/data", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookup := func(name string) (string, error) {
				if name == tc.missing {
					return "", exec.ErrNotFound
				}
				return "/usr/bin/" + name, nil
			}
			_, err := detect(lookup, tc.goos, tc.home, tc.data)
			if (err != nil) != tc.wantError {
				t.Fatalf("error %v", err)
			}
		})
	}
}
func TestSandboxCommands(t *testing.T) {
	r, err := detect(func(name string) (string, error) { return "/usr/bin/" + name, nil }, "linux", "/home/test", "/data/test")
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []Scope{User, System, "installation:work"} {
		for _, op := range []Operation{List, Remotes, RemoteLS} {
			t.Run(string(scope)+"/"+string(op), func(t *testing.T) {
				q := Query{Scope: scope, Operation: op}
				if op == RemoteLS {
					q.Remote = "flathub"
				}
				args, env, err := r.invocation(q)
				if err != nil {
					t.Fatal(err)
				}
				joined := " " + strings.Join(args, " ") + " "
				for _, flag := range []string{"--ro-bind / /", "--unshare-all", "--unshare-user", "--unshare-net", "--unshare-pid", "--cap-drop ALL", "--disable-userns", "--new-session", "--die-with-parent", "--proc /proc", "--dev /dev", "--tmpfs /run", "--tmpfs /tmp", "--chdir /", "--clearenv"} {
					if !strings.Contains(joined, " "+flag+" ") {
						t.Errorf("missing %s: %v", flag, args)
					}
				}
				if strings.Contains(joined, "--dev-bind") {
					t.Fatal("host devices exposed")
				}
				if !reflect.DeepEqual(env, controlledEnv("/home/test", "/data/test")) {
					t.Fatalf("environment %v", env)
				}
				index := -1
				for i, arg := range args {
					if arg == "/usr/bin/flatpak" {
						index = i
					}
				}
				expected := []string{"/usr/bin/flatpak", string(op), scopeFlag(scope)}
				switch op {
				case List:
					expected = append(expected, "--app", "--columns=ref:full,origin:full,version:full")
				case Remotes:
					expected = append(expected, "--show-disabled", "--columns=name:full,options:full")
				case RemoteLS:
					expected = append(expected, "--cached", "--app", "--all", "--arch=*", "--columns=ref:full,version:full", "flathub")
				}
				if index < 0 || !reflect.DeepEqual(args[index:], expected) {
					t.Fatalf("unexpected product command %v", args)
				}
			})
		}
	}
	for _, q := range []Query{{Operation: "install", Scope: System}, {Operation: List, Scope: ""}, {Operation: List, Scope: System, Remote: "flathub"}, {Operation: RemoteLS, Scope: System, Remote: "--help"}, {Operation: RemoteLS, Scope: System, Remote: "https://evil"}} {
		if _, _, err := r.invocation(q); err == nil {
			t.Fatalf("accepted %+v", q)
		}
	}
}
func TestExecRunner(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, mode string
		timeout    time.Duration
		cancel     bool
		want       error
		wantText   string
	}{
		{name: "success", mode: "success", wantText: "org.example.App/x86_64/stable\t\n"},
		{name: "stdout cap", mode: "stdout", want: ErrOutputLimit},
		{name: "stderr cap", mode: "stderr", want: ErrOutputLimit},
		{name: "timeout", mode: "sleep", timeout: 30 * time.Millisecond, want: context.DeadlineExceeded},
		{name: "cancelled", mode: "sleep", cancel: true, want: context.Canceled},
		{name: "sandbox unsupported", mode: "failure", want: ErrExecution},
		{name: "warning", mode: "warning", want: ErrExecution},
		{name: "valid stdout nonzero exit", mode: "valid-failure", want: ErrExecution},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := detect(func(name string) (string, error) { return "/usr/bin/" + name, nil }, "linux", "/home/test", "")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			var executed *exec.Cmd
			r.command = func(ctx context.Context, path string, args ...string) *exec.Cmd {
				calls++
				if path != "/usr/bin/bwrap" {
					t.Fatalf("unisolated execution %s", path)
				}
				executed = exec.CommandContext(ctx, executable, "-test.run=^TestProcessHelper$", "--", tc.mode)
				return executed
			}
			r.maxOutput = 1024
			if tc.timeout != 0 {
				r.timeout = tc.timeout
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			got, err := r.Run(ctx, Query{Operation: List, Scope: System})
			if tc.want == nil {
				if err != nil || got != tc.wantText {
					t.Fatalf("%q %v", got, err)
				}
			} else if !errors.Is(err, tc.want) || got != "" {
				t.Fatalf("got %q, %v want empty evidence, %v", got, err, tc.want)
			}
			if calls > 1 {
				t.Fatal("unsafe retry")
			}
			if executed != nil {
				if executed.Stdin != nil || len(executed.ExtraFiles) != 0 || executed.Dir != "/" {
					t.Fatal("unsafe subprocess descriptors or working directory")
				}
				if !reflect.DeepEqual(executed.Env, controlledEnv("/home/test", "/home/test/.local/share")) {
					t.Fatalf("inherited process environment: %v", executed.Env)
				}
			}
		})
	}
}

// The listener is a startup barrier, not a timing guess: a connection can only
// arrive after the helper process is running. No host inspection tool is used.
func helperListener(t *testing.T) *net.TCPListener {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := listener.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return listener
}

func helperRunner(t *testing.T, mode, address string) (*ExecRunner, **exec.Cmd) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r, err := detect(func(name string) (string, error) { return "/usr/bin/" + name, nil }, "linux", "/home/test", "")
	if err != nil {
		t.Fatal(err)
	}
	r.timeout = 15 * time.Second
	var command *exec.Cmd
	r.command = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		command = exec.CommandContext(ctx, executable, "-test.run=^TestProcessHelper$", "--", mode, address)
		return command
	}
	return r, &command
}

type helperResult struct {
	output string
	err    error
}

func runHelper(t *testing.T, r *ExecRunner, ctx context.Context, cancel context.CancelFunc) <-chan helperResult {
	t.Helper()
	result := make(chan helperResult, 1)
	done := make(chan struct{})
	go func() {
		output, err := r.Run(ctx, Query{Operation: List, Scope: System})
		result <- helperResult{output, err}
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("runner did not stop during cleanup")
		}
	})
	return result
}

func TestExecRunnerCancellationAfterStart(t *testing.T) {
	listener := helperListener(t)
	r, command := helperRunner(t, "started", listener.Addr().String())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := runHelper(t, r, ctx, cancel)
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cancel() // Accept proves startup; this is not pre-start cancellation.
	select {
	case got := <-result:
		if got.output != "" || !errors.Is(got.err, context.Canceled) {
			t.Fatalf("got %q, %v; want empty evidence and cancellation", got.output, got.err)
		}
		if (*command).ProcessState == nil {
			t.Fatal("helper was not waited")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runtime cancellation did not return promptly")
	}
}

func TestExecRunnerWaitDelay(t *testing.T) {
	controller := false
	for _, arg := range os.Args {
		controller = controller || arg == "--waitdelay-controller"
	}
	if !controller {
		// Keep Linux subreaper state confined to an isolated test process. It
		// adopts the orphan pipe-holder so cleanup can terminate AND reap it.
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestExecRunnerWaitDelay$", "--", "--waitdelay-controller")
		cmd.Env = controlledEnv("/home/test", "/home/test/.local/share")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("pipe shutdown controller: %v\n%s", err, output)
		}
		return
	}
	const prSetChildSubreaper = 36
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0, 0, 0, 0); errno != 0 {
		t.Fatal(errno)
	}
	listener := helperListener(t)
	r, _ := helperRunner(t, "pipe-parent", listener.Addr().String())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := runHelper(t, r, ctx, cancel)
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var pid int
	if _, err := fmt.Fscanln(conn, &pid); err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = conn.Close() // EOF tells the pipe-holder to exit.
		process, err := os.FindProcess(pid)
		if err != nil {
			t.Error(err)
			return
		}
		// Kill is restricted to the PID received from our own helper. Wait
		// proves cleanup, including no orphan zombie left for host PID 1.
		_ = process.Kill()
		if _, err := process.Wait(); err != nil {
			t.Errorf("reap pipe-holder: %v", err)
		}
	})
	select {
	case got := <-result:
		if got.output != "" || !errors.Is(got.err, ErrExecution) || !errors.Is(got.err, exec.ErrWaitDelay) {
			t.Fatalf("got %q, %v; want empty evidence and WaitDelay execution error", got.output, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("descendant-held pipe was not bounded by WaitDelay")
	}
}

func TestProcessHelper(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "--" {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	switch os.Args[index+1] {
	case "started", "pipe-holder":
		conn, err := net.Dial("tcp", os.Args[index+2])
		if err != nil {
			os.Exit(2)
		}
		fmt.Fprintln(conn, os.Getpid())
		fmt.Print("org.example.App/x86_64/stable\tflathub\t1\n")
		var stop [1]byte
		_, _ = conn.Read(stop[:])
		_ = conn.Close()
	case "pipe-parent":
		child := exec.Command(os.Args[0], "-test.run=^TestProcessHelper$", "--", "pipe-holder", os.Args[index+2])
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		child.Env = os.Environ()
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
	case "valid-failure":
		fmt.Print("org.example.App/x86_64/stable\tflathub\t1\n")
		os.Exit(1)
	case "success":
		fmt.Print("org.example.App/x86_64/stable\t\n")
	case "stdout":
		fmt.Print(strings.Repeat("x", 8192))
	case "stderr":
		fmt.Fprint(os.Stderr, strings.Repeat("x", 8192))
	case "sleep":
		time.Sleep(30 * time.Second)
	case "warning":
		fmt.Fprint(os.Stderr, "cache warning")
	case "failure":
		fmt.Fprint(os.Stderr, "unsupported bwrap flag")
		os.Exit(1)
	}
	os.Exit(0)
}
