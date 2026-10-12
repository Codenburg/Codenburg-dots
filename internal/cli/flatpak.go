package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Codenburg/Codenburg-dots/internal/plan"
	"github.com/Codenburg/Codenburg-dots/internal/providers/flatpak"
	"github.com/Codenburg/Codenburg-dots/internal/software"
)

// FlatpakInspector supplies local read-only evidence. Inventory may return valid
// rows alongside diagnostics; Installed does not prove uniqueness with partial lists.
type FlatpakInspector interface {
	plan.FlatpakInspector
	Inventory(context.Context, flatpak.Scope) (flatpak.Inventory, error)
}

// RunReadOnly adds lazy Flatpak dependencies without changing Run or interactive
// apply. The factory runs only after complete selection validation; nil fails closed.
// Main must route apply through RunInteractive, never this inspection entry point.
func RunReadOnly(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	detector Detector,
	inspector PackageInspector,
	newFlatpak func() (FlatpakInspector, error),
) error {
	if len(args) == 0 || (args[0] != "plan" && args[0] != "flatpak") {
		return Run(args, stdout, stderr, detector, inspector)
	}
	d, err := parseReadOnly(args)
	if err != nil {
		return err
	}
	catalog := software.BuiltinCatalog()
	if args[0] == "plan" {
		if len(d.IDs) == 0 {
			return errors.New("usage: cdots plan <software-id>...")
		}
		explicit := d.Provider != ""
		if !explicit {
			d.Provider = software.APT
		}
		resolved, err := software.Resolve(catalog, d)
		if err != nil {
			return fmt.Errorf("resolve plan: %w", err)
		}
		if !explicit {
			for _, item := range resolved {
				for _, v := range item.Software.Variants {
					if v.Provider != software.APT {
						return fmt.Errorf("software %q requires explicit --provider=apt or --provider=flatpak", item.Software.ID)
					}
				}
			}
		}
		if d.Provider == software.APT {
			if err := requireAPT(detector, inspector); err != nil {
				return err
			}
			result, err := plan.Build(ctx, catalog, d, inspector)
			return errors.Join(err, printPlan(stdout, result))
		}
	} else {
		if len(d.IDs) > 1 {
			return errors.New("usage: cdots flatpak [filters] <application-id>")
		}
		id := "org.example.App"
		if len(d.IDs) == 1 {
			id = d.IDs[0]
		}
		s := flatpak.Selection{ID: id, Scope: d.Scope, Remote: d.Remote, Arch: d.Arch, Branch: d.Branch}
		if err := s.Validate(); err != nil {
			return err
		}
		if len(d.IDs) == 0 && (d.Remote != "" || d.Arch != "" || d.Branch != "") {
			return errors.New("Flatpak remote/arch/branch filters require an application ID")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if newFlatpak == nil {
		return errors.New("Flatpak read-only inspection is unavailable (inspector factory required)")
	}
	fp, err := newFlatpak()
	if err != nil {
		return fmt.Errorf("Flatpak read-only inspection unavailable: %w", err)
	}
	if fp == nil {
		return errors.New("Flatpak read-only inspection is unavailable")
	}
	if args[0] == "plan" {
		result, err := plan.BuildFlatpak(ctx, catalog, d, fp)
		return errors.Join(err, printFlatpakPlan(stdout, result))
	}
	return inspectFlatpak(ctx, d, stdout, fp)
}

// parseReadOnly accepts --name=value or --name value before IDs, rejecting
// duplicates and trailing flags rather than silently treating them as ignored flags.
func parseReadOnly(args []string) (software.Desired, error) {
	d := software.Desired{IDs: []string{}}
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			d.IDs = append(d.IDs, arg)
			continue
		}
		if len(d.IDs) > 0 {
			return d, errors.New("flags must precede IDs")
		}
		name, value, hasValue := strings.Cut(arg, "=")
		valid := name == "--scope" || name == "--remote" || name == "--arch" || name == "--branch"
		if args[0] == "plan" && name == "--provider" {
			valid = true
		}
		if !valid {
			return d, fmt.Errorf("unknown flag %q", name)
		}
		if seen[name] {
			return d, fmt.Errorf("duplicate flag %q", name)
		}
		seen[name] = true
		if !hasValue {
			i++
			if i >= len(args) {
				return d, fmt.Errorf("flag %q requires a value", name)
			}
			value = args[i]
		}
		if value == "" || strings.HasPrefix(value, "-") {
			return d, fmt.Errorf("invalid value for %q", name)
		}
		switch name {
		case "--provider":
			d.Provider = software.Provider(value)
		case "--scope":
			d.Scope = flatpak.Scope(value)
		case "--remote":
			d.Remote = value
		case "--arch":
			d.Arch = value
		case "--branch":
			d.Branch = value
		}
	}
	return d, nil
}

func inspectFlatpak(ctx context.Context, d software.Desired, w io.Writer, fp FlatpakInspector) error {
	var b strings.Builder
	failures := []error{}
	if len(d.IDs) == 1 {
		s := flatpak.Selection{ID: d.IDs[0], Scope: d.Scope, Remote: d.Remote, Arch: d.Arch, Branch: d.Branch}
		printSelection(&b, s)
		e, err := fp.Inspect(ctx, s)
		failures = append(failures, err)
		switch e.State {
		case flatpak.Installed, flatpak.Available:
		case flatpak.Unavailable, flatpak.Unknown, flatpak.Ambiguous:
			failures = append(failures, fmt.Errorf("Flatpak observation is %s", e.State))
		default:
			failures = append(failures, fmt.Errorf("malformed Flatpak state %q", e.State))
		}
		fmt.Fprintf(&b, "State: %s (observed matches; incomplete lists cannot prove uniqueness)\n", display(string(e.State)))
		printRefs(&b, "Installed", e.Installed)
		printRefs(&b, "Candidate (cached; freshness/installability unknown)", e.Candidates)
		failures = append(failures, diagnosticText(&b, e.Diagnostics)...)
		for _, refs := range [][]flatpak.Ref{e.Installed, e.Candidates} {
			for _, ref := range refs {
				failures = append(failures, ref.Validate())
			}
		}
	} else {
		scopes := []flatpak.Scope{d.Scope}
		if d.Scope == "" {
			scopes = []flatpak.Scope{flatpak.User, flatpak.System}
		}
		for _, scope := range scopes {
			fmt.Fprintf(&b, "Scope: %s\n", scope)
			inv, err := fp.Inventory(ctx, scope)
			failures = append(failures, err)
			state := "complete local inventory"
			if err != nil || len(inv.Diagnostics) > 0 {
				state = "incomplete local inventory"
			}
			fmt.Fprintf(&b, "State: %s\n", state)
			if len(inv.Remotes) == 0 {
				b.WriteString("Remotes: none observed\n")
			}
			lines := []string{}
			for _, r := range inv.Remotes {
				lines = append(lines, fmt.Sprintf("Remote: scope=%s name=%s disabled=%t\n", r.Scope, r.Name, r.Disabled))
			}
			sort.Strings(lines)
			for _, line := range lines {
				b.WriteString(line)
			}
			printRefs(&b, "Installed", inv.Installed)
			failures = append(failures, diagnosticText(&b, inv.Diagnostics)...)
		}
	}
	sort.SliceStable(failures, func(i, j int) bool { return fmt.Sprint(failures[i]) < fmt.Sprint(failures[j]) })
	if err := errors.Join(failures...); err != nil {
		fmt.Fprintf(&b, "Diagnostic: %s\n", err)
	}
	_, writeErr := io.WriteString(w, b.String())
	return errors.Join(errors.Join(failures...), writeErr)
}

func printFlatpakPlan(w io.Writer, p plan.FlatpakPlan) error {
	var b strings.Builder
	for i, entry := range p.Entries {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "Software: %s (%s)\nProvider: %s\nDesired: %s\n", entry.SoftwareID, entry.DisplayName, entry.Provider, entry.Desired)
		printSelection(&b, entry.Selection)
		fmt.Fprintf(&b, "State: %s\n", display(string(entry.Current.State)))
		printRefs(&b, "Installed", entry.Current.Installed)
		printRefs(&b, "Candidate (cached; freshness/installability unknown)", entry.Current.Candidates)
		diagnosticText(&b, entry.Current.Diagnostics)
		fmt.Fprintf(&b, "Action: %s\n", entry.Action)
		if entry.Err != nil {
			fmt.Fprintf(&b, "Diagnostic: %s\n", entry.Err)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func printSelection(b *strings.Builder, s flatpak.Selection) {
	scope := string(s.Scope)
	if scope == "" {
		scope = "user + default-system"
	}
	filter := func(s string) string {
		if s == "" {
			return "unspecified"
		}
		return s
	}
	fmt.Fprintf(b, "Requested: id=%s scope=%s remote=%s arch=%s branch=%s\n", s.ID, scope, filter(s.Remote), filter(s.Arch), filter(s.Branch))
}

func printRefs(b *strings.Builder, label string, refs []flatpak.Ref) {
	lines := []string{}
	for _, r := range refs {
		lines = append(lines, fmt.Sprintf("%s ref: scope=%s id=%s arch=%s branch=%s origin=%s version=%s\n", label, r.Scope, r.ID, r.Arch, r.Branch, r.Remote, display(r.Version)))
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		fmt.Fprintf(b, "%s refs: none observed\n", label)
	}
	for _, line := range lines {
		b.WriteString(line)
	}
}

func diagnosticText(b *strings.Builder, ds []flatpak.Diagnostic) []error {
	lines := []string{}
	failures := []error{}
	for _, d := range ds {
		err := d.Err
		if err == nil {
			err = errors.New("malformed diagnostic without error")
		}
		cause := fmt.Errorf("scope=%s operation=%s remote=%s: %w", d.Scope, d.Operation, d.Remote, err)
		failures = append(failures, cause)
		lines = append(lines, "Diagnostic: "+cause.Error()+"\n")
	}
	sort.Strings(lines)
	for _, line := range lines {
		b.WriteString(line)
	}
	sort.SliceStable(failures, func(i, j int) bool { return failures[i].Error() < failures[j].Error() })
	return failures
}
