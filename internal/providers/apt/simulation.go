package apt

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Codenburg/Codenburg-dots/internal/packages"
)

const simulationTimeout = 30 * time.Second
const maxCommandOutput = 1024 * 1024

// Operation is one reported APT action; empty versions or architecture are unknown.
// Kind: install, upgrade, removal, configuration, unresolved.
// Risk: normal, informational, review-required, high, unresolved. Uncertainty
// is independent of risk so incomplete metadata cannot erase known high risk.
type Operation struct {
	Kind, Package, Architecture, OldVersion, NewVersion string
	Requested                                           bool
	Risk                                                string
	Uncertainty                                         []string
}

// Preview is snapshot evidence, never authorization to execute a transaction.
// Requested means a resolved operand, not necessarily direct catalog software.
type Preview struct {
	Requests                     []string
	Operations                   []Operation
	Simulation                   Diagnostic
	Metadata                     []Diagnostic
	Warnings, ReviewRequirements []string
	Unresolved                   bool
}

// Diagnostic preserves bounded command output, including failed commands.
type Diagnostic struct {
	Command        string
	Stdout, Stderr string
	Err            error
}

// Simulate previews all resolved desired-present packages in one apt-get call.
// Valid review-required/high-risk previews return nil error; uncertainty does not.
func (p *Provider) Simulate(parent context.Context, names []string) (Preview, error) {
	ctx, cancel := context.WithTimeout(parent, simulationTimeout)
	defer cancel()
	v := Preview{Requests: []string{}, Operations: []Operation{}, Metadata: []Diagnostic{},
		Warnings: []string{"simulation is a non-atomic snapshot; APT output is not a stable machine API",
			"non-root APT configuration visibility may differ"}, ReviewRequirements: []string{}}
	issues := []error{}
	finish := func() (Preview, error) {
		err := errors.Join(issues...)
		v.Unresolved = err != nil
		if err != nil {
			v.Warnings = append(v.Warnings, err.Error())
			v.Operations = append(v.Operations, Operation{Kind: "unresolved", Risk: "unresolved", Uncertainty: []string{err.Error()}})
		}
		sort.Strings(v.ReviewRequirements)
		return v, err
	}
	operands := map[string]bool{}
	bases := map[string]string{}
	for _, name := range names {
		if !packageOperand.MatchString(name) || strings.HasSuffix(name, "+") || strings.HasSuffix(name, "-") {
			issues = append(issues, fmt.Errorf("invalid simulation operand %q", name))
			continue
		}
		base, _, _ := strings.Cut(name, ":")
		if previous, ok := bases[base]; ok && previous != name {
			issues = append(issues, fmt.Errorf("ambiguous requested identity %q", base))
		}
		bases[base] = name
		operands[name] = true
	}
	if len(issues) != 0 {
		return finish()
	}
	for name := range operands {
		v.Requests = append(v.Requests, name)
	}
	sort.Strings(v.Requests)
	if len(v.Requests) == 0 {
		issues = append(issues, errors.New("empty requested transaction"))
		return finish()
	}
	archDiagnostic := p.diagnostic(ctx, "dpkg", "--print-architecture")
	v.Metadata = append(v.Metadata, archDiagnostic)
	native := strings.TrimSpace(archDiagnostic.Stdout)
	if archDiagnostic.Err != nil || !architectureToken.MatchString(native) {
		issues = append(issues, errors.Join(errors.New("native architecture unavailable"), archDiagnostic.Err))
		return finish()
	}
	inspected := map[string]packages.Info{}
	inspector := New(transactionRunner(func(ctx context.Context, name string, args ...string) (string, string, error) {
		d := p.diagnostic(ctx, name, args...)
		v.Metadata = append(v.Metadata, d)
		return d.Stdout, d.Stderr, d.Err
	}))
	for _, name := range v.Requests {
		base, arch, _ := strings.Cut(name, ":")
		if arch != "" && arch != native && arch != "all" {
			issues = append(issues, fmt.Errorf("unsupported requested architecture %q", arch))
			continue
		}
		info, err := inspector.Inspect(ctx, name)
		if err != nil {
			issues = append(issues, err)
			continue
		}
		inspected[base] = info
		if info.InstalledVersion == "" && info.CandidateVersion == "" {
			issues = append(issues, fmt.Errorf("unavailable requested package %s", name))
		}
	}
	if len(issues) != 0 {
		return finish()
	}
	args := []string{"--simulate", "-o", "APT::Get::Simulate=true", "-o", "Debug::NoLocking=true", "-o", "APT::Get::AutomaticRemove=false",
		"-o", "APT::Get::Show-User-Simulation-Note=false", "install", "--"}
	v.Simulation = p.diagnostic(ctx, "apt-get", append(args, v.Requests...)...)
	ops, lists, err := parseSimulation(v.Simulation.Stdout)
	v.Operations = ops
	essential, held := map[string]bool{}, map[string]bool{}
	for _, list := range lists {
		risk := "review-required"
		target := held
		switch list.heading {
		case "WARNING: The following essential packages will be removed.":
			risk = "high"
			target = essential
		case "The following held packages will be changed:":
		default:
			continue
		}
		v.Warnings = append(v.Warnings, list.heading+" "+strings.Join(list.names, " "))
		v.ReviewRequirements = append(v.ReviewRequirements, list.heading+" ("+risk+")")
		for _, name := range list.names {
			base, _, _ := strings.Cut(name, ":")
			target[base] = true
		}
	}
	issues = append(issues, v.Simulation.Err, err)
	for _, line := range strings.Split(v.Simulation.Stdout, "\n") {
		if m := newestLine.FindStringSubmatch(line); m != nil {
			base, _, _ := strings.Cut(m[1], ":")
			info, ok := inspected[base]
			if !ok || info.InstalledVersion != m[2] {
				issues = append(issues, fmt.Errorf("newest-version evidence contradicts inspection for %s", m[1]))
			}
		}
	}
	cache := map[string]metadata{}
	changed := map[string]bool{}
	for i := range v.Operations {
		op := &v.Operations[i]
		paired := false
		for _, other := range ops {
			if other.Package == op.Package && (other.Kind == "install" || other.Kind == "upgrade") {
				paired = true
			}
		}
		op.Requested = bases[op.Package] != ""
		op.Risk = "informational"
		if op.Requested {
			op.Risk = "normal"
		}
		if op.Kind == "upgrade" || op.Kind == "removal" || held[op.Package] {
			op.Risk = "review-required"
		}
		if essential[op.Package] {
			op.Risk = "high"
		}
		if op.Architecture != "" && op.Architecture != native && op.Architecture != "all" {
			issues = append(issues, fmt.Errorf("unsupported operation architecture %q", op.Architecture))
		}
		if op.Kind != "configuration" {
			changed[op.Package] = true
		}
		if info, ok := inspected[op.Package]; ok {
			_, requestedArch, _ := strings.Cut(bases[op.Package], ":")
			if requestedArch != "" && op.Architecture != "" && requestedArch != op.Architecture {
				issues = append(issues, errors.New("operation contradicts requested architecture"))
			}
			if op.Kind == "removal" || (op.Kind == "install" && info.InstalledVersion != "") {
				issues = append(issues, errors.New("operation contradicts requested installed state"))
			}
			if op.Kind == "upgrade" && info.InstalledVersion != op.OldVersion {
				issues = append(issues, errors.New("operation contradicts inspected old version"))
			}
			standalone := op.Kind == "configuration" && !paired
			if standalone && info.InstalledVersion != op.NewVersion {
				issues = append(issues, errors.New("configuration contradicts inspected installed version"))
			}
			if op.NewVersion != "" && !standalone && info.CandidateVersion != op.NewVersion {
				issues = append(issues, errors.New("operation contradicts inspected candidate version"))
			}
		}
		identity := op.Package
		if op.Architecture != "" {
			identity += ":" + op.Architecture
		}
		assess := func(old bool, version string) {
			key := fmt.Sprintf("%t/%s/%s", old, identity, version)
			m, ok := cache[key]
			if !ok {
				m = p.transactionMetadata(ctx, identity, version, old)
				cache[key] = m
				v.Metadata = append(v.Metadata, m.diagnostic)
			}
			if m.arch != "" {
				if op.Architecture == "" {
					op.Architecture = m.arch
				}
				if m.arch != native && m.arch != "all" {
					issues = append(issues, errors.New("unsupported metadata architecture"))
				}
			}
			if old && essential[op.Package] && !m.essential {
				conflict := fmt.Errorf("essential-removal warning lacks matching Essential evidence for %s", identity)
				op.Uncertainty = append(op.Uncertainty, conflict.Error())
				issues = append(issues, conflict)
			}
			if m.critical {
				op.Risk = "high"
			}
			if m.conffiles {
				op.Risk = maxReviewRisk(op.Risk)
			}
			if m.err != nil {
				if op.Risk == "normal" || op.Risk == "informational" {
					op.Risk = "unresolved"
				}
				op.Uncertainty = append(op.Uncertainty, m.err.Error())
				issues = append(issues, m.err)
			}
		}
		if op.OldVersion != "" {
			assess(true, op.OldVersion)
		}
		if op.NewVersion != "" && (op.Kind != "configuration" || paired) {
			assess(false, op.NewVersion)
		}
		if op.Kind == "configuration" {
			for _, other := range ops {
				if other.Package == op.Package && (other.Kind == "install" || other.Kind == "upgrade") {
					if op.Risk != "high" && other.Risk != "" {
						op.Risk = other.Risk
					}
					op.Uncertainty = append(op.Uncertainty, other.Uncertainty...)
				}
			}
			if !paired {
				op.Risk = maxReviewRisk(op.Risk)
				assess(true, op.NewVersion)
			}
		}
		if op.Risk == "review-required" || op.Risk == "high" {
			v.ReviewRequirements = append(v.ReviewRequirements, op.Package+": "+op.Kind+" ("+op.Risk+")")
		}
	}
	identities := map[string]string{}
	kinds := map[string]bool{}
	for _, op := range v.Operations {
		if previous, ok := identities[op.Package]; ok && previous != op.Architecture {
			issues = append(issues, fmt.Errorf("ambiguous multiarch operation %s", op.Package))
		}
		identities[op.Package] = op.Architecture
		kinds[op.Package+"/"+op.Kind] = true
	}
	for _, op := range v.Operations {
		name := op.Package
		unpacked := kinds[name+"/install"] || kinds[name+"/upgrade"]
		if kinds[name+"/removal"] && (unpacked || kinds[name+"/configuration"]) {
			issues = append(issues, fmt.Errorf("contradictory operations for %s", name))
		}
	}
	for _, name := range v.Requests {
		base, _, _ := strings.Cut(name, ":")
		info := inspected[base]
		if !changed[base] && info.InstalledVersion == "" {
			issues = append(issues, fmt.Errorf("requested install %s absent from preview", base))
		}
	}
	return finish()
}

type transactionRunner func(context.Context, string, ...string) (string, string, error)

func (r transactionRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	return r(ctx, name, args...)
}

func maxReviewRisk(risk string) string {
	if risk == "high" {
		return risk
	}
	return "review-required"
}

func (p *Provider) diagnostic(ctx context.Context, name string, args ...string) Diagnostic {
	out, stderr, err := p.runner.Run(ctx, name, args...)
	if len(out) > maxCommandOutput {
		out = out[:maxCommandOutput]
		err = errors.Join(err, errors.New("stdout limit exceeded"))
	}
	if len(stderr) > maxCommandOutput {
		stderr = stderr[:maxCommandOutput]
		err = errors.Join(err, errors.New("stderr limit exceeded"))
	}
	if err == nil && strings.TrimSpace(stderr) != "" {
		err = fmt.Errorf("unexpected stderr from %s", name)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "E:") || strings.HasPrefix(line, "W:") || strings.HasPrefix(line, "WARNING:") {
			if name != "apt-get" || line != "WARNING: The following essential packages will be removed." {
				err = errors.Join(err, fmt.Errorf("unassessed diagnostic from %s: %s", name, line))
			}
		}
	}
	err = errors.Join(err, ctx.Err())
	return Diagnostic{Command: name + " " + strings.Join(args, " "), Stdout: out, Stderr: stderr, Err: err}
}
