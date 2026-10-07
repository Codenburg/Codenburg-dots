package main

import (
	"fmt"
	"os"

	"github.com/Codenburg/Codenburg-dots/internal/cli"
	"github.com/Codenburg/Codenburg-dots/internal/providers/apt"
	"github.com/Codenburg/Codenburg-dots/internal/system"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout, os.Stderr, systemDetector{}, apt.New(apt.ExecRunner{})); err != nil {
		fmt.Fprintln(os.Stderr, "cdots:", err)
		os.Exit(2)
	}
}

type systemDetector struct{}

func (systemDetector) Detect() (system.Info, error) { return system.Detect() }
