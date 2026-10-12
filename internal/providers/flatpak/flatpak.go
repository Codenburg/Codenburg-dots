// Package flatpak provides isolated, read-only application evidence.
package flatpak

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type Scope string

const (
	User   Scope = "user"
	System Scope = "system"
)

// Selection leaves optional filters empty rather than inventing a branch or architecture.
// An empty scope searches user and default-system installations.
type Selection struct {
	ID                   string
	Scope                Scope
	Remote, Arch, Branch string
}

// Ref identity includes every field except Version. Version is opaque and may be empty.
type Ref struct {
	Scope                             Scope
	Remote, ID, Arch, Branch, Version string
}
type Remote struct {
	Scope    Scope
	Name     string
	Disabled bool
}
type State string

const (
	Installed   State = "installed"
	Available   State = "available"
	Unavailable State = "unavailable"
	Unknown     State = "unknown"
	Ambiguous   State = "ambiguous"
)

type Diagnostic struct {
	Scope     Scope
	Operation Operation
	Remote    string
	Err       error
}
type Inventory struct {
	Installed   []Ref
	Remotes     []Remote
	Diagnostics []Diagnostic
}
type Evidence struct {
	State                 State
	Installed, Candidates []Ref
	Diagnostics           []Diagnostic
}
type Operation string

const (
	List     Operation = "list"
	Remotes  Operation = "remotes"
	RemoteLS Operation = "remote-ls"
)

// Query is a closed inspection vocabulary, not arbitrary command arguments.
type Query struct {
	Operation Operation
	Scope     Scope
	Remote    string
}

// Runner implementations must enforce read-only, offline execution. Inject fixtures in tests.
type Runner interface {
	Run(context.Context, Query) (string, error)
}
type Provider struct{ runner Runner }

func New(runner Runner) *Provider { return &Provider{runner: runner} }

// Inventory defaults to the system scope. Named installations are queried only explicitly.
// Operational and malformed-data errors are retained as diagnostics beside valid rows.
func (p *Provider) Inventory(ctx context.Context, scope Scope) (Inventory, error) {
	if scope == "" {
		scope = System
	}
	if err := scope.Validate(); err != nil {
		return Inventory{}, err
	}
	if p.runner == nil {
		return Inventory{}, errors.New("flatpak runner is required")
	}
	result := Inventory{Installed: []Ref{}, Remotes: []Remote{}, Diagnostics: []Diagnostic{}}
	for _, op := range []Operation{Remotes, List} {
		q := Query{Operation: op, Scope: scope}
		output, err := p.runner.Run(ctx, q)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, diagnostic(q, err))
			continue
		}
		rows, errs := table(output, map[Operation]int{Remotes: 2, List: 3}[op])
		for _, err := range errs {
			result.Diagnostics = append(result.Diagnostics, diagnostic(q, err))
		}
		for _, row := range rows {
			if op == Remotes {
				remote, err := parseRemote(scope, row)
				if err != nil {
					result.Diagnostics = append(result.Diagnostics, diagnostic(q, err))
					continue
				}
				result.Remotes = append(result.Remotes, remote)
				continue
			}
			ref, err := parseRef(scope, row[1], row[0], row[2])
			if err != nil {
				result.Diagnostics = append(result.Diagnostics, diagnostic(q, err))
				continue
			}
			result.Installed = append(result.Installed, ref)
		}
	}
	sortRefs(result.Installed)
	sort.Slice(result.Remotes, func(i, j int) bool { return result.Remotes[i].Name < result.Remotes[j].Name })
	result.Installed = uniqueRefs(result.Installed, &result.Diagnostics, Query{Operation: List, Scope: scope})
	seen := map[string]bool{}
	remotes := []Remote{}
	for _, r := range result.Remotes {
		if seen[r.Name] {
			result.Diagnostics = append(result.Diagnostics, diagnostic(Query{Operation: Remotes, Scope: scope}, fmt.Errorf("duplicate remote %q", r.Name)))
			continue
		}
		seen[r.Name] = true
		remotes = append(remotes, r)
	}
	// Conflicting duplicate rows cannot establish whether a remote is enabled.
	for i := range remotes {
		for _, d := range result.Diagnostics {
			if d.Operation == Remotes {
				remotes[i].Disabled = true
			}
		}
	}
	result.Remotes = remotes
	return result, nil
}

// Inspect reports positive evidence independently of optional version metadata.
// Unavailable means no match in complete local observations, not global nonexistence.
func (p *Provider) Inspect(ctx context.Context, s Selection) (Evidence, error) {
	if err := s.Validate(); err != nil {
		return Evidence{}, err
	}
	scopes := []Scope{s.Scope}
	if s.Scope == "" {
		scopes = []Scope{User, System}
	}
	result := Evidence{State: Unknown, Installed: []Ref{}, Candidates: []Ref{}, Diagnostics: []Diagnostic{}}
	for _, scope := range scopes {
		inventory, err := p.Inventory(ctx, scope)
		if err != nil {
			return result, err
		}
		result.Diagnostics = append(result.Diagnostics, inventory.Diagnostics...)
		for _, ref := range inventory.Installed {
			if matches(s, ref) {
				result.Installed = append(result.Installed, ref)
			}
		}
		for _, remote := range inventory.Remotes {
			if remote.Disabled || (s.Remote != "" && remote.Name != s.Remote) {
				continue
			}
			q := Query{Operation: RemoteLS, Scope: scope, Remote: remote.Name}
			output, err := p.runner.Run(ctx, q)
			if err != nil {
				result.Diagnostics = append(result.Diagnostics, diagnostic(q, err))
				continue
			}
			rows, errs := table(output, 2)
			for _, err := range errs {
				result.Diagnostics = append(result.Diagnostics, diagnostic(q, err))
			}
			refs := []Ref{}
			for _, row := range rows {
				ref, err := parseRef(scope, remote.Name, row[0], row[1])
				if err != nil {
					result.Diagnostics = append(result.Diagnostics, diagnostic(q, err))
					continue
				}
				refs = append(refs, ref)
			}
			sortRefs(refs)
			refs = uniqueRefs(refs, &result.Diagnostics, q)
			for _, ref := range refs {
				if matches(s, ref) {
					result.Candidates = append(result.Candidates, ref)
				}
			}
		}
	}
	sortRefs(result.Installed)
	sortRefs(result.Candidates)
	switch {
	case len(result.Installed) > 1 || len(result.Candidates) > 1:
		result.State = Ambiguous
	case len(result.Installed) == 1:
		result.State = Installed
	case len(result.Diagnostics) > 0:
		result.State = Unknown
	case len(result.Candidates) == 1:
		result.State = Available
	default:
		result.State = Unavailable
	}
	return result, nil
}

var operandPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
var idPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)+\.[A-Za-z_][A-Za-z0-9_-]*$`)

func validOperand(s string) bool {
	return len(s) > 0 && len(s) <= 255 && operandPattern.MatchString(s) && s != "." && s != ".."
}
func (s Scope) Validate() error {
	if s == User || s == System {
		return nil
	}
	if strings.HasPrefix(string(s), "installation:") && validOperand(strings.TrimPrefix(string(s), "installation:")) {
		return nil
	}
	return fmt.Errorf("invalid flatpak scope %q", s)
}
func (s Selection) Validate() error {
	if len(s.ID) > 255 || !idPattern.MatchString(s.ID) {
		return fmt.Errorf("invalid flatpak application ID %q", s.ID)
	}
	if s.Scope != "" {
		if err := s.Scope.Validate(); err != nil {
			return err
		}
	}
	for _, value := range []string{s.Remote, s.Arch, s.Branch} {
		if value != "" && !validOperand(value) {
			return fmt.Errorf("invalid flatpak selection operand %q", value)
		}
	}
	return nil
}

// Validate requires a complete application identity; it does not compare versions.
func (r Ref) Validate() error {
	if !validOperand(r.Remote) || !validOperand(r.Arch) || !validOperand(r.Branch) {
		return errors.New("flatpak ref requires remote, architecture, and branch")
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	return (Selection{ID: r.ID, Scope: r.Scope, Remote: r.Remote, Arch: r.Arch, Branch: r.Branch}).Validate()
}

func matches(s Selection, r Ref) bool {
	return s.ID == r.ID && (s.Remote == "" || s.Remote == r.Remote) && (s.Arch == "" || s.Arch == r.Arch) && (s.Branch == "" || s.Branch == r.Branch)
}
func diagnostic(q Query, err error) Diagnostic {
	return Diagnostic{Scope: q.Scope, Operation: q.Operation, Remote: q.Remote, Err: err}
}
func refKey(r Ref) string {
	return string(r.Scope) + "/" + r.Remote + "/" + r.ID + "/" + r.Arch + "/" + r.Branch
}
func sortRefs(refs []Ref) {
	sort.Slice(refs, func(i, j int) bool { return refKey(refs[i]) < refKey(refs[j]) })
}
func uniqueRefs(refs []Ref, diagnostics *[]Diagnostic, q Query) []Ref {
	out := []Ref{}
	for _, r := range refs {
		if len(out) > 0 && refKey(out[len(out)-1]) == refKey(r) {
			*diagnostics = append(*diagnostics, diagnostic(q, fmt.Errorf("duplicate ref %q", refKey(r))))
			// Conflicting versions cannot be trusted, but presence is still proven.
			if out[len(out)-1].Version != r.Version {
				out[len(out)-1].Version = ""
			}
			continue
		}
		out = append(out, r)
	}
	return out
}
