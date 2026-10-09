//go:build linux

package apt

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// NewApplyProvider constructs the production apply-only evidence backend lazily.
// Every query (including no-op inspection and post-verification) uses trusted
// executable paths, a clean environment and fresh native configuration checks.
// It neither requests elevation nor grants execution authority. New/ExecRunner
// retain their existing read-only behavior for non-apply commands.
func NewApplyProvider() *Provider {
	return newApplyProvider(checkApplyMetadataHost, exec.CommandContext)
}

func newApplyProvider(check func() error, command func(context.Context, string, ...string) *exec.Cmd) *Provider {
	return New(applyMetadataRunner{
		checkHost: check,
		runner:    executionRunner{runner: ExecRunner{}, command: command},
	})
}

type applyMetadataRunner struct {
	checkHost func() error
	runner    executionRunner
}

func (r applyMetadataRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if r.checkHost == nil || r.runner.command == nil {
		return "", "", errors.New("missing apply evidence dependencies")
	}
	if err := r.checkHost(); err != nil {
		return "", "", applyProfileFailure(ctx, err)
	}
	out, stderr, err := r.runner.Run(ctx, name, args...)
	// Discard output if profile trust changed while reading evidence. This is
	// still a snapshot, not a lock on trusted root/package-script side effects.
	if checkErr := r.checkHost(); checkErr != nil {
		return "", "", applyProfileFailure(ctx, checkErr)
	}
	if ctx.Err() != nil {
		return "", "", ctx.Err()
	}
	// A command-local timeout/cancellation is not a native false/absence either.
	if errors.Is(err, context.Canceled) {
		return "", "", context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "", "", context.DeadlineExceeded
	}
	return out, stderr, err
}

// Profile rejection is categorically different from a native query's normal
// exit 1 (unequal versions/absent package). Never unwrap the query OR checker
// error: either could expose ExitCode and be normalized into trusted evidence.
// Retain only the safe checker diagnostic and known cancellation sentinels.
func applyProfileFailure(ctx context.Context, checkErr error) error {
	diagnostic := fmt.Errorf("apply native profile validation failed: %v", checkErr)
	cancellation := ctx.Err()
	if cancellation == nil {
		switch {
		case errors.Is(checkErr, context.Canceled):
			cancellation = context.Canceled
		case errors.Is(checkErr, context.DeadlineExceeded):
			cancellation = context.DeadlineExceeded
		}
	}
	return errors.Join(diagnostic, cancellation)
}

func checkApplyMetadataHost() error {
	// Read-only trust checks do not require effective UID 0. In particular,
	// checking our executable or requiring execution privilege here would move
	// the root gate before review. Executor alone owns that post-approval gate.
	for _, path := range []string{
		"/usr/bin/dpkg-query", "/usr/bin/dpkg", "/usr/bin/apt-cache",
		"/usr/bin/apt-config", "/usr/bin/apt-get",
	} {
		if err := trustedExecutionPath(path); err != nil {
			return err
		}
	}
	return checkNativeDpkgConfig(nativeDpkgConfigFS())
}
