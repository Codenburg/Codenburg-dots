package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Codenburg/Codenburg-dots/internal/packages"
	"github.com/Codenburg/Codenburg-dots/internal/system"
)

type SystemInfo = system.Info
type PackageInfo = packages.Info
type Detector interface{ Detect() (system.Info, error) }
type PackageInspector interface {
	Inspect(context.Context, string) (packages.Info, error)
}

func Run(args []string, stdout, stderr io.Writer, detector Detector, inspector PackageInspector) error {
	if len(args) == 0 {
		return printSystem(stdout, detector)
	}
	switch args[0] {
	case "system":
		if len(args) != 1 {
			return fmt.Errorf("usage: cdots system")
		}
		return printSystem(stdout, detector)
	case "package":
		if len(args) != 2 {
			return fmt.Errorf("usage: cdots package <name>")
		}
		info, err := detector.Detect()
		if err != nil {
			return fmt.Errorf("detect system: %w", err)
		}
		if !info.Supported {
			return fmt.Errorf("unsupported distribution %q (ID_LIKE: %s)", info.ID, info.IDLike)
		}
		if !info.APTAvailable {
			return fmt.Errorf("APT inspection requires dpkg-query, apt-cache, and dpkg in PATH")
		}
		if inspector == nil {
			return fmt.Errorf("APT package inspection is unavailable")
		}
		pkg, err := inspector.Inspect(context.Background(), args[1])
		if err != nil {
			return fmt.Errorf("inspect package %s: %w", args[1], err)
		}
		_, err = fmt.Fprintf(stdout, "Package: %s\nStatus: %s\nInstalled version: %s\nCandidate version: %s\n", pkg.Name, pkg.Status, display(pkg.InstalledVersion), display(pkg.CandidateVersion))
		return err
	default:
		return fmt.Errorf("usage: cdots [system | package <name>]")
	}
}

func printSystem(w io.Writer, detector Detector) error {
	info, err := detector.Detect()
	if err != nil {
		return fmt.Errorf("detect system: %w", err)
	}
	status := "unsupported"
	if info.Supported {
		status = "supported"
	}
	_, err = fmt.Fprintf(w, "Distribution: %s\nID: %s\nID_LIKE: %s\nArchitecture: %s (raw: %s)\nSupport: %s\nAPT: %s\n", display(info.Name), display(info.ID), display(info.IDLike), display(info.Architecture), display(info.RawArchitecture), status, map[bool]string{true: "available", false: "unavailable"}[info.APTAvailable])
	return err
}
func display(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return s
}
