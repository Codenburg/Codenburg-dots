package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Codenburg/Codenburg-dots/internal/cli"
	"github.com/Codenburg/Codenburg-dots/internal/providers/apt"
	"github.com/Codenburg/Codenburg-dots/internal/system"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--internal-apt-guard" {
		if err := apt.RunInternalGuard(os.Args[2:], os.Stdin); err != nil {
			fmt.Fprintln(os.Stderr, "cdots guard:", err)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	provider := commandProvider(os.Args[1:], apt.NewApplyProvider)
	executor := apt.NewExecutor(provider, os.Stdin, os.Stdout, os.Stderr)
	if err := cli.RunInteractive(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, systemDetector{}, provider, executor); err != nil {
		fmt.Fprintln(os.Stderr, "cdots:", err)
		os.Exit(cli.ExitCode(err))
	}
}

// Only apply uses authoritative evidence isolation. Ordinary read-only commands
// keep their existing provider/environment behavior. Both factories are lazy.
func commandProvider(args []string, newApply func() *apt.Provider) *apt.Provider {
	if len(args) > 0 && args[0] == "apply" {
		return newApply()
	}
	return apt.New(apt.ExecRunner{})
}

type systemDetector struct{}

func (systemDetector) Detect() (system.Info, error) { return system.Detect() }
