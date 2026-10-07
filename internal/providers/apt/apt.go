package apt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/Codenburg/Codenburg-dots/internal/packages"
)

const commandTimeout = 5 * time.Second

type Runner interface {
	Run(context.Context, string, ...string) (stdout, stderr string, err error)
}
type ExecRunner struct{}

func (ExecRunner) Run(parent context.Context, name string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(parent, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = cLocaleEnvironment()
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("%s timed out: %w", name, ctx.Err())
	}
	if err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), stderr.String(), nil
}

func cLocaleEnvironment() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "LC_ALL=") {
			env = append(env, item)
		}
	}
	return append(env, "LC_ALL=C")
}

func Available() (bool, error) {
	for _, name := range []string{"dpkg-query", "apt-cache", "dpkg"} {
		if _, err := exec.LookPath(name); err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				return false, nil
			}
			return false, fmt.Errorf("locate %s: %w", name, err)
		}
	}
	return true, nil
}

var packageOperand = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*(?::[a-z0-9][a-z0-9-]*)?$`)

type Provider struct{ runner Runner }

func New(r Runner) *Provider {
	if r == nil {
		r = ExecRunner{}
	}
	return &Provider{runner: r}
}

func (p *Provider) Inspect(ctx context.Context, name string) (packages.Info, error) {
	if !packageOperand.MatchString(name) {
		return packages.Info{}, fmt.Errorf("invalid APT package name %q", name)
	}
	const format = "${db:Status-Abbrev}\\t${Version}\\n"
	out, diagnostic, err := p.runner.Run(ctx, "dpkg-query", "-W", "-f="+format, "--", name)
	installed := ""
	if err == nil {
		installed, err = installedVersion(out)
		if err != nil {
			return packages.Info{}, fmt.Errorf("dpkg-query for %s: %w", name, err)
		}
	} else if !exitCode(err, 1) || out != "" || strings.TrimSpace(diagnostic) != "dpkg-query: no packages found matching "+name {
		return packages.Info{}, fmt.Errorf("inspect installed package %s: %w (%s)", name, err, strings.TrimSpace(diagnostic))
	}
	policy, _, err := p.runner.Run(ctx, "apt-cache", "policy", name)
	if err != nil {
		return packages.Info{}, fmt.Errorf("inspect APT candidate for %s: %w", name, err)
	}
	candidate, ok := candidateVersion(policy)
	if !ok && strings.TrimSpace(policy) == "" {
		candidate, ok = "(none)", true
	}
	if !ok {
		return packages.Info{}, fmt.Errorf("apt-cache returned no candidate field for %s", name)
	}
	atLeast := false
	if installed != "" && candidate != "" && candidate != "(none)" {
		_, _, cmpErr := p.runner.Run(ctx, "dpkg", "--compare-versions", installed, "ge", candidate)
		if cmpErr == nil {
			atLeast = true
		} else if !exitCode(cmpErr, 1) {
			return packages.Info{}, fmt.Errorf("compare Debian versions for %s: %w", name, cmpErr)
		}
	}
	status := packages.DeriveState(installed, candidate, candidate != "(none)", atLeast)
	return packages.Info{Name: name, Status: status, InstalledVersion: installed, CandidateVersion: nonNone(candidate)}, nil
}

func installedVersion(out string) (string, error) {
	fields := strings.Split(strings.TrimSuffix(out, "\n"), "\t")
	if len(fields) != 2 || strings.ContainsAny(fields[1], "\r\n ") {
		return "", fmt.Errorf("malformed status/version record")
	}
	status := fields[0]
	if len(status) != 3 || !strings.ContainsRune("uihrp", rune(status[0])) || !strings.ContainsRune("ncHUFWti", rune(status[1])) || (status[2] != ' ' && status[2] != 'R') {
		return "", fmt.Errorf("malformed status abbreviation %q", status)
	}
	if status[2] != ' ' {
		return "", fmt.Errorf("package status %q requires repair; installed state is not reliable", status)
	}
	if status[1] != 'i' {
		return "", nil
	}
	if fields[1] == "" {
		return "", fmt.Errorf("installed status has no version")
	}
	return fields[1], nil
}

func candidateVersion(policy string) (string, bool) {
	for _, line := range strings.Split(policy, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Candidate:") {
			candidate := strings.TrimSpace(strings.TrimPrefix(line, "Candidate:"))
			return candidate, candidate != ""
		}
	}
	return "", false
}
func nonNone(s string) string {
	if s == "(none)" {
		return ""
	}
	return s
}
func exitCode(err error, code int) bool {
	var e interface{ ExitCode() int }
	return errors.As(err, &e) && e.ExitCode() == code
}
