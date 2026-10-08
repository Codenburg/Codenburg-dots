package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Codenburg/Codenburg-dots/internal/packages"
	"github.com/Codenburg/Codenburg-dots/internal/plan"
	"github.com/Codenburg/Codenburg-dots/internal/software"
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
		if err := requireAPT(detector, inspector); err != nil {
			return err
		}
		pkg, err := inspector.Inspect(context.Background(), args[1])
		if err != nil {
			return fmt.Errorf("inspect package %s: %w", args[1], err)
		}
		_, err = fmt.Fprintf(stdout, "Package: %s\nStatus: %s\nInstalled version: %s\nCandidate version: %s\n", pkg.Name, pkg.Status, display(pkg.InstalledVersion), display(pkg.CandidateVersion))
		return err
	case "plan":
		if len(args) < 2 {
			return fmt.Errorf("usage: cdots plan <software-id>...")
		}
		catalog := software.BuiltinCatalog()
		desired := software.Desired{IDs: args[1:], Provider: software.APT}
		// Validate logical selection before host checks so an unknown ID remains
		// actionable even on an unsupported host. Build also enforces resolution.
		if _, err := software.Resolve(catalog, desired); err != nil {
			return fmt.Errorf("resolve plan: %w", err)
		}
		if err := requireAPT(detector, inspector); err != nil {
			return err
		}
		result, err := plan.Build(context.Background(), catalog, desired, inspector)
		return errors.Join(err, printPlan(stdout, result))
	case "preflight":
		if len(args) < 2 {
			return fmt.Errorf("usage: cdots preflight <software-id>...")
		}
		return runPreflight(args[1:], stdout, detector, inspector, software.BuiltinCatalog())
	default:
		return fmt.Errorf("usage: cdots [system | package <name> | plan <software-id>... | preflight <software-id>...]")
	}
}

func requireAPT(detector Detector, inspector PackageInspector) error {
	_, err := requireAPTSystem(detector, inspector)
	return err
}

func requireAPTSystem(detector Detector, inspector PackageInspector) (system.Info, error) {
	info, err := detector.Detect()
	if err != nil {
		return info, fmt.Errorf("detect system: %w", err)
	}
	if !info.Supported {
		return info, fmt.Errorf("unsupported distribution %q (ID_LIKE: %s)", info.ID, info.IDLike)
	}
	if !info.APTAvailable {
		return info, fmt.Errorf("APT inspection requires dpkg-query, apt-cache, and dpkg in PATH")
	}
	if inspector == nil {
		return info, fmt.Errorf("APT package inspection is unavailable")
	}
	return info, nil
}

func printPlan(w io.Writer, result plan.Plan) error {
	for i, entry := range result.Entries {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "Software: %s (%s)\nProvider: %s\nPackage: %s\nDesired: %s\nInstalled version: %s\nCandidate version: %s\nAction: %s\n",
			entry.SoftwareID, entry.DisplayName, entry.Provider, entry.Package, entry.Desired,
			display(entry.Current.InstalledVersion), display(entry.Current.CandidateVersion), entry.Action); err != nil {
			return err
		}
		if entry.Err != nil {
			if _, err := fmt.Fprintf(w, "Diagnostic: %s\n", entry.Err); err != nil {
				return err
			}
		}
	}
	return nil
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
