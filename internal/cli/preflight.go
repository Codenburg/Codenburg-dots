package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Codenburg/Codenburg-dots/internal/providers/apt"
	"github.com/Codenburg/Codenburg-dots/internal/software"
	"github.com/Codenburg/Codenburg-dots/internal/system"
)

// Simulation owns inspection and bounded execution; the CLI only resolves and renders.
type aptSimulator interface {
	Simulate(context.Context, []string) (apt.Preview, error)
}

func runPreflight(
	ids []string,
	w io.Writer,
	detector Detector,
	inspector PackageInspector,
	catalog software.Catalog,
) error {
	resolved, err := software.Resolve(catalog, software.Desired{IDs: ids, Provider: software.APT})
	if err != nil {
		return fmt.Errorf("resolve preflight: %w", err)
	}
	info, err := requireAPTSystem(detector, inspector)
	if err != nil {
		return err
	}
	arch, _ := system.NormalizeArchitecture(info.RawArchitecture)
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("unsupported architecture %q for APT preflight", info.RawArchitecture)
	}
	simulator, ok := inspector.(aptSimulator)
	if !ok {
		return fmt.Errorf("APT transaction simulation is unavailable")
	}
	operands := make([]string, 0, len(resolved))
	for _, entry := range resolved {
		operands = append(operands, entry.Variant.Identifier)
	}
	operands = sortedUnique(operands)
	preview, simulationErr := simulator.Simulate(context.Background(), operands)
	if preview.Unresolved && simulationErr == nil {
		simulationErr = errors.New("APT preflight evidence is unresolved")
	}
	return errors.Join(simulationErr, printPreflight(w, ids, resolved, preview, simulationErr))
}

func printPreflight(
	w io.Writer,
	ids []string,
	resolved []software.Resolved,
	preview apt.Preview,
	previewErr error,
) error {
	// Build in memory so the only fallible output operation is checked once.
	var b strings.Builder
	fmt.Fprintf(&b, "Requested software: %s\nProvider: apt\n", strings.Join(sortedUnique(ids), ", "))
	direct := make(map[string]bool, len(ids))
	for _, id := range ids {
		direct[id] = true
	}
	sources := make(map[string][]string, len(resolved))
	directOperands := make(map[string]bool, len(ids))
	packages := make([]string, 0, len(resolved))
	for _, entry := range resolved {
		name := entry.Variant.Identifier
		source := "catalog dependency "
		if direct[entry.Software.ID] {
			source = "requested software "
			directOperands[name] = true
		}
		sources[name] = append(sources[name], source+entry.Software.ID+" ("+entry.Software.Name+")")
		packages = append(packages, name)
	}
	for _, name := range sortedUnique(packages) {
		fmt.Fprintf(&b, "Resolved package: %s\nSource: %s\n", name, strings.Join(sortedUnique(sources[name]), "; "))
	}
	fmt.Fprintf(&b, "APT operands: %s\n", strings.Join(sortedUnique(preview.Requests), ", "))

	status := "preview-complete"
	blocks := make([]string, 0, len(preview.Operations))
	for _, op := range preview.Operations {
		source := operationSource(op, sources)
		kind := op.Kind
		if kind == "install" {
			kind = "installation"
			directOperand := directOperands[op.Package] || directOperands[op.Package+":"+op.Architecture]
			if !directOperand {
				kind = "dependency installation"
			}
		}
		risk := op.Risk
		if risk == "high" {
			risk = "high-risk"
			status = "high-risk"
		}
		if risk == "review-required" && status != "high-risk" {
			status = "review-required"
		}
		var block strings.Builder
		fmt.Fprintf(&block, "\nOperation: %s\nPackage: %s\nSource: %s\nArchitecture: %s\nOld version: %s\nNew version: %s\nRisk: %s\n",
			kind, display(op.Package), source, display(op.Architecture), display(op.OldVersion), display(op.NewVersion), display(risk))
		for _, uncertainty := range op.Uncertainty {
			fmt.Fprintf(&block, "Uncertainty: %s\n", uncertainty)
		}
		if op.Kind == "removal" {
			fmt.Fprintf(&block, "Review requirement: %s: operation-specific removal approval required\n", display(op.Package))
		}
		if op.Risk == "high" {
			fmt.Fprintf(&block, "Review requirement: %s: additional safeguards required for high-risk %s\n", display(op.Package), op.Kind)
		}
		// The sort key includes the complete record, retaining duplicate evidence.
		blocks = append(blocks, display(op.Package)+"\x00"+block.String())
	}
	sort.Strings(blocks)
	for _, block := range blocks {
		_, text, _ := strings.Cut(block, "\x00")
		b.WriteString(text)
	}
	fmt.Fprintf(&b, "\nOperations retained: %d\n", len(preview.Operations))
	for _, warning := range preview.Warnings {
		fmt.Fprintf(&b, "Warning: %s\n", warning)
	}
	for _, requirement := range preview.ReviewRequirements {
		fmt.Fprintf(&b, "Review requirement: %s\n", requirement)
	}
	for _, diagnostic := range preview.Metadata {
		if diagnostic.Err != nil || strings.TrimSpace(diagnostic.Stderr) != "" {
			printPreflightDiagnostic(&b, "Metadata diagnostic", diagnostic)
		}
	}
	result := "not run"
	if preview.Simulation.Command != "" {
		result = "succeeded"
		if preview.Simulation.Err != nil {
			result = "failed"
		}
		fmt.Fprintf(&b, "Simulation command: %s\n", preview.Simulation.Command)
	}
	fmt.Fprintf(&b, "Simulation result: %s\n", result)
	if preview.Simulation.Err != nil || strings.TrimSpace(preview.Simulation.Stderr) != "" {
		printPreflightDiagnostic(&b, "Simulation diagnostic", preview.Simulation)
	}
	if len(preview.ReviewRequirements) != 0 && status == "preview-complete" {
		status = "review-required"
	}
	if preview.Unresolved || previewErr != nil {
		status = "unresolved"
		fmt.Fprintln(&b, "Assessment: not safely actionable; evidence is incomplete or failed")
	}
	if previewErr != nil {
		fmt.Fprintf(&b, "Diagnostic: %s\n", previewErr)
	}
	fmt.Fprintf(&b, "Overall status: %s\n", status)
	fmt.Fprintln(&b, "Simulation is a snapshot, not execution authority. Execution is not implemented.")
	fmt.Fprintln(&b, "No changes applied.")
	_, err := io.WriteString(w, b.String())
	return err
}

func operationSource(op apt.Operation, sources map[string][]string) string {
	values := sources[op.Package]
	if len(values) == 0 && op.Architecture != "" {
		values = sources[op.Package+":"+op.Architecture]
	}
	if len(values) != 0 {
		return strings.Join(sortedUnique(values), "; ")
	}
	if op.Kind == "unresolved" && op.Package == "" {
		return "transaction evidence"
	}
	return "additional APT-selected package"
}

func printPreflightDiagnostic(b *strings.Builder, label string, d apt.Diagnostic) {
	fmt.Fprintf(b, "%s: %s\n", label, display(d.Command))
	if d.Err != nil {
		fmt.Fprintf(b, "Diagnostic error: %s\n", d.Err)
	}
	if d.Stdout != "" {
		fmt.Fprintf(b, "Diagnostic stdout:\n%s\n", d.Stdout)
	}
	if d.Stderr != "" {
		fmt.Fprintf(b, "Diagnostic stderr:\n%s\n", d.Stderr)
	}
}

func sortedUnique(values []string) []string {
	result := append([]string{}, values...)
	sort.Strings(result)
	unique := result[:0]
	for _, value := range result {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	return unique
}
