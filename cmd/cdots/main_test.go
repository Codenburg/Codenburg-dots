package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Codenburg/Codenburg-dots/internal/providers/apt"
)

func TestMainHiddenGuardFailClosed(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainDispatchHelper$", "--", "--internal-apt-guard", "invalid")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "cdots guard:") || strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("hidden dispatch changed: %v %q %q", err, stdout.String(), stderr.String())
	}
}

func TestMainHelpDoesNotInspectOrExecute(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainDispatchHelper$", "--", "--help")
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "cdots apply") || !strings.Contains(string(output), "APT 2.6.1") {
		t.Fatalf("help failed: %v %s", err, output)
	}
}

func TestMainSelectsApplyOnlyBackend(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		apply bool
	}{
		{name: "apply", args: []string{"apply", "bash"}, apply: true},
		{name: "apply usage", args: []string{"apply"}, apply: true},
		{name: "help", args: []string{"--help"}},
		{name: "package", args: []string{"package", "bash"}},
		{name: "plan", args: []string{"plan", "bash"}},
		{name: "preflight", args: []string{"preflight", "bash"}},
		{name: "system", args: []string{"system"}},
		{name: "empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			selected := apt.New(nil)
			provider := commandProvider(tc.args, func() *apt.Provider { calls++; return selected })
			if (calls == 1) != tc.apply || (provider == selected) != tc.apply {
				t.Fatal("main did not select the apply-only factory")
			}
		})
	}
}

func TestMainDispatchHelper(t *testing.T) {
	i := 0
	for i < len(os.Args) && os.Args[i] != "--" {
		i++
	}
	if i == len(os.Args) {
		return
	}
	os.Args = append([]string{"cdots"}, os.Args[i+1:]...)
	main()
	os.Exit(0)
}
