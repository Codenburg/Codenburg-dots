package apt

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Codenburg/Codenburg-dots/internal/packages"
)

// ApplyState freezes requested inspection and its bound safety records.
type ApplyState struct {
	Info                 packages.Info
	Installed, Candidate PackageEvidence
}

// InspectApplyState binds both versions' metadata for requested-state drift
// detection, including satisfied operands excluded from simulation.
func (p *Provider) InspectApplyState(ctx context.Context, name string) (ApplyState, error) {
	info, err := p.InspectApply(ctx, name)
	state := ApplyState{Info: info}
	if err != nil {
		return state, err
	}
	if info.InstalledVersion != "" {
		m := p.transactionMetadata(ctx, name, info.InstalledVersion, true)
		if m.err != nil {
			return state, m.err
		}
		state.Installed = m.evidence
	}
	if info.CandidateVersion != "" {
		m := p.transactionMetadata(ctx, name, info.CandidateVersion, false)
		if m.err != nil {
			return state, m.err
		}
		state.Candidate = m.evidence
	}
	return state, ctx.Err()
}

// InspectApply adds strict installed-status evidence to existing inspection.
// In particular, unpacked/half-configured packages are not verified removals or
// fresh install operands. Ordinary read-only Inspect keeps its existing API.
func (p *Provider) InspectApply(ctx context.Context, name string) (packages.Info, error) {
	info, err := p.Inspect(ctx, name)
	if err != nil {
		return info, err
	}
	if info.InstalledVersion != "" {
		m := p.transactionMetadata(ctx, name, info.InstalledVersion, true)
		if m.err != nil {
			return info, m.err
		}
		return info, ctx.Err()
	}
	out, stderr, err := p.runner.Run(ctx, "dpkg-query", "-W", "-f=${Status}\\t${Version}\\t${Architecture}\\n", "--", name)
	if ctx.Err() != nil {
		return info, ctx.Err()
	}
	if err != nil {
		if exitCode(err, 1) && out == "" && strings.TrimSpace(stderr) == "dpkg-query: no packages found matching "+name {
			return info, nil
		}
		return info, fmt.Errorf("inspect absence %s: %w", name, errors.Join(err, errors.New("unreliable absence evidence")))
	}
	fields := strings.Split(strings.TrimSuffix(out, "\n"), "\t")
	if stderr != "" || len(fields) != 3 {
		return info, errors.New("malformed absence evidence")
	}
	if fields[0] != "deinstall ok config-files" && fields[0] != "unknown ok not-installed" {
		return info, errors.New("partial or unknown package state")
	}
	_, arch, _ := strings.Cut(name, ":")
	if !architectureToken.MatchString(fields[2]) || (arch != "" && arch != fields[2]) {
		return info, errors.New("unbound absent architecture")
	}
	return info, nil
}

// CompareVersions uses Debian's ordering, not lexical or semantic-version ordering.
// Exit 1 means false; any other failure is unavailable safety evidence.
func (p *Provider) CompareVersions(ctx context.Context, left, relation, right string) (bool, error) {
	if !versionToken.MatchString(left) || !versionToken.MatchString(right) {
		return false, errors.New("invalid Debian version")
	}
	switch relation {
	case "lt", "eq", "gt":
	default:
		return false, errors.New("unsupported version relation")
	}
	out, stderr, err := p.runner.Run(ctx, "dpkg", "--compare-versions", left, relation, right)
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if out != "" || stderr != "" {
		return false, errors.Join(errors.New("unexpected version comparison output"), err)
	}
	if err == nil {
		return true, nil
	}
	if exitCode(err, 1) {
		return false, nil
	}
	return false, fmt.Errorf("compare Debian versions: %w", err)
}

// ValidateApply is deliberately stricter than read-only preflight. It validates
// bound safety evidence, but grants no authorization and performs no mutation.
func (p *Provider) ValidateApply(ctx context.Context, v Preview) error {
	if v.Unresolved || v.Simulation.Err != nil {
		return errors.New("unresolved transaction")
	}
	for _, d := range v.Metadata {
		if d.Err != nil {
			return fmt.Errorf("unresolved metadata: %w", d.Err)
		}
	}
	requests := map[string]string{}
	for _, name := range v.Requests {
		if !packageOperand.MatchString(name) || strings.HasSuffix(name, "-") || strings.HasSuffix(name, "+") {
			return errors.New("invalid requested identity")
		}
		base, arch, _ := strings.Cut(name, ":")
		if _, ok := requests[base]; ok {
			return errors.New("duplicate or ambiguous request")
		}
		requests[base] = arch
	}
	if len(requests) == 0 || len(v.Operations) == 0 {
		return errors.New("incomplete transaction")
	}
	changes := map[string]Operation{}
	configurations := map[string]Operation{}
	for _, op := range v.Operations {
		if !packageOperand.MatchString(op.Package) || strings.Contains(op.Package, ":") || !architectureToken.MatchString(op.Architecture) {
			return errors.New("missing or invalid operation identity")
		}
		if len(op.Uncertainty) != 0 {
			return errors.New("uncertain operation evidence")
		}
		switch op.Risk {
		case "normal", "informational", "review-required", "high":
		default:
			return errors.New("unknown operation risk")
		}
		arch, requested := requests[op.Package]
		if op.Requested != requested || (requested && arch != "" && arch != op.Architecture) {
			return errors.New("contradictory requested identity")
		}
		if op.Kind == "configuration" {
			if _, ok := configurations[op.Package]; ok {
				return errors.New("duplicate configuration")
			}
			configurations[op.Package] = op
		} else {
			if _, ok := changes[op.Package]; ok {
				return errors.New("contradictory package operations")
			}
			changes[op.Package] = op
		}
		switch op.Kind {
		case "install":
			if op.OldVersion != "" || op.Installed != (PackageEvidence{}) {
				return errors.New("install contradicts installed evidence")
			}
			if err := boundCandidate(op); err != nil {
				return err
			}
		case "upgrade":
			if err := boundInstalled(op, op.OldVersion); err != nil {
				return err
			}
			if err := boundCandidate(op); err != nil {
				return err
			}
			newer, err := p.CompareVersions(ctx, op.NewVersion, "gt", op.OldVersion)
			if err != nil {
				return err
			}
			if !newer {
				return errors.New("downgrade or reinstall forbidden")
			}
		case "removal":
			if op.NewVersion != "" || op.Candidate != (PackageEvidence{}) || requested {
				return errors.New("contradictory removal")
			}
			if err := boundInstalled(op, op.OldVersion); err != nil {
				return err
			}
			if op.Installed.Essential || op.Installed.Protected {
				return errors.New("essential or protected removal forbidden")
			}
		case "configuration":
			if !versionToken.MatchString(op.NewVersion) {
				return errors.New("missing configuration version")
			}
		default:
			return errors.New("unsupported operation")
		}
	}
	for name, op := range configurations {
		if change, ok := changes[name]; ok {
			oldVersionMatches := op.OldVersion == "" || op.OldVersion == change.OldVersion
			if change.Kind == "removal" || !oldVersionMatches || op.Architecture != change.Architecture || op.NewVersion != change.NewVersion || op.Installed != change.Installed || op.Candidate != change.Candidate {
				return errors.New("configuration contradicts package change")
			}
		} else {
			if op.OldVersion != "" && op.OldVersion != op.NewVersion {
				return errors.New("contradictory standalone configuration")
			}
			if err := boundInstalled(op, op.NewVersion); err != nil {
				return err
			}
			if op.Candidate != (PackageEvidence{}) {
				return errors.New("unexpected standalone candidate evidence")
			}
		}
	}
	for name := range requests {
		if op, ok := changes[name]; !ok || op.Kind != "install" {
			return errors.New("requested missing install absent from transaction")
		}
	}
	return ctx.Err()
}

func boundInstalled(op Operation, version string) error {
	e := op.Installed
	bound := e.Known && e.Package == op.Package && e.Architecture == op.Architecture && e.Version == version
	if !bound || !versionToken.MatchString(version) {
		return errors.New("missing or unbound installed evidence")
	}
	if e.Status != "install ok installed" || e.Held {
		return errors.New("held or partial installed state forbidden")
	}
	return nil
}
func boundCandidate(op Operation) error {
	e := op.Candidate
	bound := e.Known && e.Package == op.Package && e.Architecture == op.Architecture && e.Version == op.NewVersion
	if !bound || !versionToken.MatchString(op.NewVersion) || e.Status != "" || e.Held {
		return errors.New("missing or contradictory candidate evidence")
	}
	return nil
}

// SameTransaction compares canonical material authority; diagnostic prose is
// intentionally excluded. Inputs should separately pass ValidateApply.
func SameTransaction(a, b Preview) bool {
	ar, br := slices.Clone(a.Requests), slices.Clone(b.Requests)
	slices.Sort(ar)
	slices.Sort(br)
	if !slices.Equal(ar, br) || a.Unresolved != b.Unresolved || len(a.Operations) != len(b.Operations) {
		return false
	}
	ao, bo := slices.Clone(a.Operations), slices.Clone(b.Operations)
	less := func(a, b Operation) int { return strings.Compare(a.Package+"/"+a.Kind, a.Package+"/"+b.Kind) }
	slices.SortFunc(ao, less)
	slices.SortFunc(bo, less)
	for i, x := range ao {
		y := bo[i]
		if x.Kind != y.Kind || x.Package != y.Package || x.Architecture != y.Architecture || x.OldVersion != y.OldVersion || x.NewVersion != y.NewVersion || x.Requested != y.Requested || x.Risk != y.Risk || x.Installed != y.Installed || x.Candidate != y.Candidate || !slices.Equal(x.Uncertainty, y.Uncertainty) {
			return false
		}
	}
	return true
}
