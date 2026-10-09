//go:build linux

package apt_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Codenburg/Codenburg-dots/internal/apply"
	"github.com/Codenburg/Codenburg-dots/internal/providers/apt"
)

type backendReviewer struct {
	confirm func() bool
	renders int
}

func (r *backendReviewer) Render(context.Context, apt.Preview) error         { r.renders++; return nil }
func (*backendReviewer) Review(context.Context, apt.Operation) (bool, error) { return true, nil }
func (*backendReviewer) ApproveRemoval(context.Context, apt.Operation) (bool, error) {
	return true, nil
}
func (r *backendReviewer) Confirm(context.Context, apt.Preview) (bool, error) {
	return r.confirm(), nil
}

type backendExecutor func() error

func (e backendExecutor) Execute(context.Context, apt.Preview) error { return e() }

func backendCommands(t *testing.T, installed bool, shadow string) (func(context.Context, string, ...string) *exec.Cmd, string, string, *[]string) {
	t.Helper()
	dir := t.TempDir()
	state, trace := filepath.Join(dir, "host-state"), filepath.Join(dir, "environment-trace")
	if installed {
		if err := os.WriteFile(state, []byte("installed"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	calls := []string{}
	command := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		fixtureArgs := []string{"-test.run=^TestApplyBackendMetadataHelper$", "--", state, trace, shadow, name}
		fixtureArgs = append(fixtureArgs, args...)
		return exec.CommandContext(ctx, os.Args[0], fixtureArgs...)
	}
	return command, state, trace, &calls
}

func assertCleanBackendTrace(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || strings.Contains(string(data), "dirty") {
		t.Fatal("actual authoritative metadata child inherited non-allowlisted environment")
	}
}

func shadowHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Fixture-only user config and shadow DB: the host fixture remains absent.
	if err := os.WriteFile(filepath.Join(dir, ".dpkg-query.cfg"), []byte("admindir="+dir+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "status"), []byte("installed"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	return dir
}

func TestApplyBackendWorkflowRejectsShadowFalseNoAction(t *testing.T) {
	shadow := shadowHome(t)
	command, _, trace, calls := backendCommands(t, false, shadow)
	checks := 0
	p := apt.NewApplyProviderFixture(func() error { checks++; return nil }, command)
	if checks != 0 || len(*calls) != 0 {
		t.Fatal("constructor queried state")
	}
	r := &backendReviewer{confirm: func() bool { return false }}
	w := apply.Workflow{Backend: p, Reviewer: r, Executor: backendExecutor(func() error { t.Fatal("rejected transaction executed"); return nil })}
	result, err := w.Run(context.Background(), []string{"demo:amd64"})
	if result.Status == apply.NoAction {
		t.Fatal("shadow HOME falsely satisfied whole Workflow before executor")
	}
	if !errors.Is(err, apply.ErrRejected) || r.renders != 1 || result.Executed {
		t.Fatalf("host-absent package did not reach explicit review: %+v %v", result, err)
	}
	if checks < len(*calls) {
		t.Fatal("authoritative command escaped native profile validation")
	}
	assertCleanBackendTrace(t, trace)
}

func TestApplyBackendWorkflowConfigGateBeforeNoop(t *testing.T) {
	command, _, _, calls := backendCommands(t, true, "")
	checks := 0
	p := apt.NewApplyProviderFixture(func() error { checks++; return errors.New("unsafe native query profile") }, command)
	if checks != 0 || len(*calls) != 0 {
		t.Fatal("constructor not lazy")
	}
	result, err := (apply.Workflow{Backend: p}).Run(context.Background(), []string{"demo:amd64"})
	if err == nil || result.Status == apply.NoAction || len(*calls) != 0 || checks == 0 {
		t.Fatalf("unsafe profile queried or satisfied noop: %+v %v", result, err)
	}
}

func TestApplyBackendWorkflowRefreshesBeforeReinspection(t *testing.T) {
	command, _, trace, calls := backendCommands(t, false, "")
	invalid := false
	p := apt.NewApplyProviderFixture(func() error {
		if invalid {
			return errors.New("native query profile drift")
		}
		return nil
	}, command)
	approvedCalls := 0
	r := &backendReviewer{confirm: func() bool { approvedCalls = len(*calls); invalid = true; return true }}
	result, err := (apply.Workflow{Backend: p, Reviewer: r, Executor: backendExecutor(func() error { t.Fatal("unsafe reinspection executed"); return nil })}).Run(context.Background(), []string{"demo:amd64"})
	if !errors.Is(err, apply.ErrFreshReviewRequired) || result.Executed || len(*calls) != approvedCalls {
		t.Fatalf("reinspection consumed unsupported profile: %+v %v", result, err)
	}
	assertCleanBackendTrace(t, trace)
}

func TestApplyBackendWorkflowPostVerification(t *testing.T) {
	for _, tc := range []struct {
		name                string
		update, failProfile bool
	}{
		{name: "verified host update", update: true},
		{name: "unsafe post profile", update: true, failProfile: true},
		{name: "shadow cannot verify absent host package"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := shadowHome(t)
			for _, key := range []string{"DPKG_FORCE", "DPKG_ROOT", "DPKG_ADMINDIR", "APT_CONFIG", "LD_PRELOAD", "LD_LIBRARY_PATH", "GCONV_PATH", "GLIBC_TUNABLES", "DEBIAN_FRONTEND", "LC_CTYPE"} {
				t.Setenv(key, dir)
			}
			command, state, trace, calls := backendCommands(t, false, dir)
			invalid := false
			p := apt.NewApplyProviderFixture(func() error {
				if invalid {
					return errors.New("native post-verification profile drift")
				}
				return nil
			}, command)
			beforeVerify := 0
			executor := backendExecutor(func() error {
				beforeVerify = len(*calls)
				invalid = tc.failProfile
				if tc.update {
					return os.WriteFile(state, []byte("installed"), 0600)
				}
				return nil
			})
			r := &backendReviewer{confirm: func() bool { return true }}
			result, err := (apply.Workflow{Backend: p, Reviewer: r, Executor: executor}).Run(context.Background(), []string{"demo:amd64"})
			if !result.Executed {
				t.Fatalf("fixture execution never reached: %+v %v", result, err)
			}
			if tc.failProfile {
				if err == nil || result.Status == apply.Success || len(*calls) != beforeVerify {
					t.Fatalf("unsafe profile claimed successful verification: %+v %v", result, err)
				}
			} else if !tc.update {
				if !errors.Is(err, apply.ErrVerification) || result.Status == apply.Success || result.Verification[0].Verified {
					t.Fatalf("shadow DB falsely verified an absent host package: %+v %v", result, err)
				}
			} else if err != nil || result.Status != apply.Success || len(result.Verification) != 1 || !result.Verification[0].Verified {
				t.Fatalf("clean host fixture was not independently verified: %+v %v", result, err)
			}
			assertCleanBackendTrace(t, trace)
		})
	}
}

func TestApplyBackendQueryBoundaryAfterCommand(t *testing.T) {
	command, _, _, calls := backendCommands(t, true, "")
	checks := 0
	p := apt.NewApplyProviderFixture(func() error {
		checks++
		if checks > 1 {
			return errors.New("profile changed during query")
		}
		return nil
	}, command)
	result, err := (apply.Workflow{Backend: p}).Run(context.Background(), []string{"demo:amd64"})
	if err == nil || result.Status == apply.NoAction || len(*calls) != 1 {
		t.Fatalf("unsupported query result accepted: %+v %v", result, err)
	}
}

type backendExitOne struct{}

func (backendExitOne) Error() string { return "fixture checker exit one" }
func (backendExitOne) ExitCode() int { return 1 }

func TestApplyBackendProfileErrorsDominateNativeExitOne(t *testing.T) {
	for _, operation := range []string{"compare", "inspect comparison", "absence", "status absence"} {
		for _, phase := range []string{"healthy", "precheck", "postcheck", "nested checker exit"} {
			t.Run(operation+"/"+phase, func(t *testing.T) {
				checks := 0
				failureAt := 2
				if operation == "inspect comparison" || operation == "status absence" {
					failureAt = 6
				}
				if phase == "precheck" {
					failureAt = 1
				}
				p := apt.NewApplyProviderFixture(func() error {
					checks++
					if phase == "healthy" || checks != failureAt {
						return nil
					}
					if phase == "nested checker exit" || phase == "precheck" {
						return fmt.Errorf("native profile rejected: %w", backendExitOne{})
					}
					return errors.New("native profile rejected")
				}, func(ctx context.Context, path string, args ...string) *exec.Cmd {
					fixtureArgs := []string{"-test.run=^TestApplyBackendExitOneHelper$", "--", operation, path}
					return exec.CommandContext(ctx, os.Args[0], append(fixtureArgs, args...)...)
				})
				var err error
				switch operation {
				case "compare":
					var equal bool
					equal, err = p.CompareVersions(context.Background(), "1", "eq", "2")
					if equal {
						t.Fatal("unequal versions compared equal")
					}
				case "inspect comparison":
					_, err = p.Inspect(context.Background(), "demo:amd64")
				default:
					_, err = p.InspectApply(context.Background(), "demo:amd64")
				}
				if phase == "healthy" {
					if err != nil {
						t.Fatalf("healthy native exit one lost its normal semantics: %v", err)
					}
					return
				}
				if err == nil {
					t.Fatal("native exit one swallowed profile rejection")
				}
				if !strings.Contains(err.Error(), "native profile rejected") {
					t.Fatalf("safe profile diagnostic lost: %v", err)
				}
				var exit interface{ ExitCode() int }
				if errors.As(err, &exit) {
					t.Fatal("profile rejection exposed native exit classification")
				}
				if strings.Contains(err.Error(), "query-private-detail") {
					t.Fatal("rejected query diagnostic leaked")
				}
			})
		}
	}
}

func TestApplyBackendProfileRejectionRetainsCancellation(t *testing.T) {
	for _, cancellation := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cancellation.Error(), func(t *testing.T) {
			checks := 0
			p := apt.NewApplyProviderFixture(func() error {
				checks++
				if checks == 2 {
					return errors.Join(errors.New("native profile rejected"), cancellation, backendExitOne{})
				}
				return nil
			}, func(ctx context.Context, path string, args ...string) *exec.Cmd {
				fixtureArgs := []string{"-test.run=^TestApplyBackendExitOneHelper$", "--", "compare", path}
				return exec.CommandContext(ctx, os.Args[0], append(fixtureArgs, args...)...)
			})
			_, err := p.CompareVersions(context.Background(), "1", "eq", "2")
			if !errors.Is(err, cancellation) {
				t.Fatalf("profile cancellation swallowed: %v", err)
			}
			var exit interface{ ExitCode() int }
			if errors.As(err, &exit) {
				t.Fatal("cancellation retained native exit classification")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checks := 0
	p := apt.NewApplyProviderFixture(func() error {
		checks++
		if checks == 2 {
			cancel()
			return errors.New("native profile rejected")
		}
		return nil
	}, func(ctx context.Context, path string, args ...string) *exec.Cmd {
		fixtureArgs := []string{"-test.run=^TestApplyBackendExitOneHelper$", "--", "compare", path}
		return exec.CommandContext(ctx, os.Args[0], append(fixtureArgs, args...)...)
	})
	_, err := p.CompareVersions(ctx, "1", "eq", "2")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("root context cancellation lost: %v", err)
	}
}

func TestApplyBackendHealthyProfileQueryCancellation(t *testing.T) {
	for _, cancellation := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cancellation.Error(), func(t *testing.T) {
			commandCtx, cancel := context.WithCancel(context.Background())
			if errors.Is(cancellation, context.DeadlineExceeded) {
				cancel()
				commandCtx, cancel = context.WithDeadline(context.Background(), time.Time{})
			} else {
				cancel()
			}
			defer cancel()
			p := apt.NewApplyProviderFixture(func() error { return nil },
				func(context.Context, string, ...string) *exec.Cmd {
					// The command's context is already done; no subprocess starts.
					return exec.CommandContext(commandCtx, os.Args[0], "-test.run=^TestApplyBackendExitOneHelper$")
				})
			_, err := p.CompareVersions(context.Background(), "1", "eq", "2")
			if !errors.Is(err, cancellation) {
				t.Fatalf("query cancellation lost: %v", err)
			}
			var exit interface{ ExitCode() int }
			if errors.As(err, &exit) {
				t.Fatal("cancellation exposed native exit classification")
			}
		})
	}
}

func TestApplyBackendExitOneHelper(t *testing.T) {
	i := 0
	for i < len(os.Args) && os.Args[i] != "--" {
		i++
	}
	if i == len(os.Args) {
		return
	}
	args := os.Args[i+1:]
	if len(args) < 3 {
		os.Exit(120)
	}
	operation, path := args[0], args[1]
	args = args[2:]
	switch path {
	case "/usr/bin/dpkg":
		os.Exit(1)
	case "/usr/bin/apt-cache":
		fmt.Print("Candidate: 2\n")
	case "/usr/bin/dpkg-query":
		if operation == "inspect comparison" {
			fmt.Print("ii \t1\n")
			break
		}
		if operation == "status absence" && strings.Contains(args[1], "db:Status-Abbrev") {
			fmt.Print("un \t\n")
			break
		}
		fmt.Fprintln(os.Stderr, "dpkg-query: no packages found matching "+args[len(args)-1])
		os.Exit(1)
	default:
		fmt.Fprint(os.Stderr, "query-private-detail")
		os.Exit(121)
	}
	os.Exit(0)
}

// Only Go test subprocesses run here. This source-modeled helper mimics the
// supplied HOME-dependent native loader; it never launches apt/dpkg or writes
// a real package DB. Only invocation-owned fixture state/trace paths are read.
func TestApplyBackendMetadataHelper(t *testing.T) {
	i := 0
	for i < len(os.Args) && os.Args[i] != "--" {
		i++
	}
	if i == len(os.Args) {
		return
	}
	args := os.Args[i+1:]
	if len(args) < 5 {
		os.Exit(110)
	}
	state, trace, shadow, path := args[0], args[1], args[2], args[3]
	args = args[4:]
	clean := true
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key != "PATH" && key != "LC_ALL" {
			clean = false
		}
	}
	file, err := os.OpenFile(trace, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(111)
	}
	mark := "clean\n"
	if !clean {
		mark = "dirty\n"
	}
	if _, err := file.WriteString(mark); err != nil {
		os.Exit(112)
	}
	if err := file.Close(); err != nil {
		os.Exit(113)
	}
	_, err = os.Stat(state)
	installed := err == nil
	if home, ok := os.LookupEnv("HOME"); ok && shadow != "" && home == shadow {
		if data, err := os.ReadFile(filepath.Join(home, ".dpkg-query.cfg")); err == nil && strings.HasPrefix(string(data), "admindir=") {
			installed = true // the explicitly created shadow DB, not host state
		}
	}
	last := args[len(args)-1]
	switch path {
	case "/usr/bin/dpkg-query":
		if !installed {
			// A known not-installed DB record keeps simulation diagnostics reliable;
			// this is not an installed package or an invented absence-query failure.
			if args[0] == "--status" {
				fmt.Fprintln(os.Stderr, "dpkg-query: no packages found matching "+last)
				os.Exit(1)
			}
			if len(args) > 1 && strings.Contains(args[1], "db:Status-Abbrev") {
				fmt.Print("un \t\n")
			} else {
				fmt.Print("unknown ok not-installed\t\tamd64\n")
			}
			os.Exit(0)
		}
		if args[0] == "--status" {
			fmt.Print("Package: demo\nArchitecture: amd64\nVersion: 2\nStatus: install ok installed\nEssential: no\nProtected: no\n")
		} else if len(args) > 1 && strings.Contains(args[1], "db:Status-Abbrev") {
			fmt.Print("ii \t2\n")
		} else {
			fmt.Print("install ok installed\t2\tamd64\n")
		}
	case "/usr/bin/apt-cache":
		if args[0] == "policy" {
			fmt.Print("Candidate: 2\n")
		} else if args[0] == "show" {
			fmt.Print("Package: demo\nArchitecture: amd64\nVersion: 2\nEssential: no\nProtected: no\n")
		} else {
			os.Exit(114)
		}
	case "/usr/bin/dpkg":
		if args[0] == "--print-architecture" {
			fmt.Print("amd64\n")
		} else if len(args) == 4 && args[0] == "--compare-versions" {
			if args[1] != args[3] || args[2] == "lt" || args[2] == "gt" {
				os.Exit(1)
			}
		} else {
			os.Exit(115)
		}
	case "/usr/bin/apt-get":
		if args[0] != "--simulate" || !strings.Contains(strings.Join(args, " "), "APT::Get::Simulate=true") {
			os.Exit(116)
		}
		fmt.Print("0 upgraded, 1 newly installed, 0 to remove and 0 not upgraded.\nInst demo (2 Repo [amd64])\nConf demo (2 Repo [amd64])\n")
	default:
		os.Exit(117)
	}
	os.Exit(0)
}
