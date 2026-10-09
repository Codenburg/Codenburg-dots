package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Codenburg/Codenburg-dots/internal/apply"
	"github.com/Codenburg/Codenburg-dots/internal/providers/apt"
	"github.com/Codenburg/Codenburg-dots/internal/software"
	"github.com/Codenburg/Codenburg-dots/internal/system"
)

const applyWarnings = `Warning: execution supports only Linux, trusted-root, APT 2.6.1 and known-default conservative configuration; unsupported profiles fail closed.
Warning: simulation is a snapshot, not execution authority; immediate drift requires fresh review, without automatic retry.
Warning: no automatic elevation; rerun privileged only with fresh approvals. Root is checked only at execution after approval.
Warning: native APT prompts remain required. No unattended flags or persisted approval.
Warning: execution is not atomic; package scripts can have side effects. No rollback is promised.`

// RunInteractive preserves Run for existing read-only callers and passes the
// caller's cancellation context to apply. Execution is always injected; main
// alone constructs the production adapter with the actual operator stdin.
func RunInteractive(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	detector Detector,
	inspector PackageInspector,
	executor apply.Executor,
) error {
	if len(args) == 0 || args[0] != "apply" {
		return Run(args, stdout, stderr, detector, inspector)
	}
	if len(args) < 2 {
		return fmt.Errorf("usage: cdots apply <software-id>...")
	}
	r := &terminalReviewer{out: stdout}
	r.capable = func() bool { return terminalCapable(stdin) && terminalCapable(stdout) && terminalCapable(stderr) }
	r.readLine = func(ctx context.Context) (string, error) {
		f, ok := stdin.(*os.File)
		if !ok {
			return "", errors.New("terminal stdin required")
		}
		return terminalLine(ctx, f)
	}
	return runApply(
		ctx,
		args[1:],
		stdout,
		detector,
		inspector,
		software.BuiltinCatalog(),
		executor,
		r,
	)
}

func runApply(
	ctx context.Context,
	ids []string,
	out io.Writer,
	detector Detector,
	inspector PackageInspector,
	catalog software.Catalog,
	executor apply.Executor,
	reviewer *terminalReviewer,
) error {
	resolved, err := software.Resolve(catalog, software.Desired{IDs: ids, Provider: software.APT})
	if err != nil {
		return fmt.Errorf("resolve apply: %w", err)
	}
	operands := make([]string, 0, len(resolved))
	for _, entry := range resolved {
		if entry.Variant.Provider != software.APT {
			return errors.New("apply requires selected APT providers")
		}
		operands = append(operands, entry.Variant.Identifier)
	}
	info, err := requireAPTSystem(detector, inspector)
	if err != nil {
		return err
	}
	arch, _ := system.NormalizeArchitecture(info.RawArchitecture)
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("unsupported architecture %q for APT apply", info.RawArchitecture)
	}
	backend, ok := inspector.(apply.Backend)
	if !ok {
		return errors.New("APT apply inspection and simulation are unavailable")
	}
	observed := &reviewBackend{Backend: backend, states: make(map[string]apt.ApplyState)}
	renderPreview := func(p apt.Preview, previewErr error) error {
		var b strings.Builder
		for _, name := range sortedUnique(operands) {
			s := observed.states[name]
			fmt.Fprintf(&b, "Requested package state: %s status=%s installed=%s candidate=%s\n",
				name, display(string(s.Info.Status)), display(s.Info.InstalledVersion), display(s.Info.CandidateVersion))
			printEvidence(&b, "Installed evidence", s.Installed)
			printEvidence(&b, "Candidate evidence", s.Candidate)
		}
		if _, err := io.WriteString(out, b.String()); err != nil {
			return err
		}
		return printTransaction(out, ids, resolved, p, previewErr, true)
	}
	rendered := false
	reviewer.render = func(p apt.Preview) error {
		err := renderPreview(p, nil)
		if err == nil {
			rendered = true
		}
		return err
	}
	result, runErr := (apply.Workflow{Backend: observed, Reviewer: reviewer, Executor: executor}).Run(ctx, sortedUnique(operands))
	// Validation deliberately precedes approval. Retain the blocked preview even
	// when the coordinator could not call Render; never ask questions here.
	if !rendered {
		err = renderPreview(result.Preview, runErr)
	}
	reportErr := printApplyResult(out, result)
	combined := errors.Join(runErr, err, reportErr)
	if combined != nil {
		return &applyCommandError{status: result.Status, err: combined}
	}
	return nil
}

// Capture the coordinator's own requested-state inspection for review, without
// extra queries or a separate snapshot that could contradict execution evidence.
type reviewBackend struct {
	apply.Backend
	states map[string]apt.ApplyState
}

func (b *reviewBackend) InspectApplyState(ctx context.Context, name string) (apt.ApplyState, error) {
	state, err := b.Backend.InspectApplyState(ctx, name)
	b.states[name] = state
	return state, err
}

type applyCommandError struct {
	status apply.Status
	err    error
}

func (e *applyCommandError) Error() string { return e.err.Error() }
func (e *applyCommandError) Unwrap() error { return e.err }

// ExitCode preserves the legacy usage/error code for non-apply errors.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var e *applyCommandError
	if errors.As(err, &e) {
		if e.status == apply.Cancellation {
			return 130
		}
		return 1
	}
	return 2
}

func printEvidence(b *strings.Builder, label string, e apt.PackageEvidence) {
	fmt.Fprintf(b, "%s: package=%s architecture=%s version=%s known=%t essential=%t protected=%t status=%s held=%t\n",
		label, display(e.Package), display(e.Architecture), display(e.Version),
		e.Known, e.Essential, e.Protected, display(e.Status), e.Held)
}
func printApplyResult(w io.Writer, r apply.Result) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Apply result: %s\nExecution attempted: %t\n", r.Status, r.Executed)
	if r.Status == apply.NoAction {
		fmt.Fprintln(&b, "Desired-present state already satisfied; no reinstall or automatic upgrade. No confirmation or execution required.")
	}
	for _, v := range r.Verification {
		fmt.Fprintf(&b, "Verification: %s removal=%t expected=%s observed=%s status=%s verified=%t\n",
			v.Package, v.Removal, display(v.ExpectedVersion), display(v.Observed.InstalledVersion),
			display(string(v.Observed.Status)), v.Verified)
		if v.Err != nil {
			fmt.Fprintf(&b, "Verification diagnostic: %s\n", v.Err)
		}
	}
	if r.Err != nil {
		fmt.Fprintf(&b, "Diagnostic: %s\n", r.Err)
	}
	if !r.Executed {
		fmt.Fprintln(&b, "No execution attempted.")
	} else if r.Status != apply.Success {
		fmt.Fprintln(&b, "Execution stopped; observed state is reported above. Native cancellation/error cause may be uncertain; no automatic retry or rollback.")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// Capability and input injection remain package-private, with no env/flag bypass.
type terminalReviewer struct {
	out      io.Writer
	capable  func() bool
	readLine func(context.Context) (string, error)
	render   func(apt.Preview) error
}

func (r *terminalReviewer) Render(ctx context.Context, p apt.Preview) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.render(p); err != nil {
		return err
	}
	if r.capable == nil || !r.capable() {
		return errors.New("real operations require genuine input and output terminals; piping approval is not supported")
	}
	return nil
}
func (r *terminalReviewer) answer(ctx context.Context, expected, detail string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if r.capable == nil || !r.capable() {
		return false, errors.New("terminal capability lost")
	}
	if _, err := fmt.Fprintf(r.out, "%s\nType %q to approve (anything else cancels): ", detail, expected); err != nil {
		return false, err
	}
	line, err := r.readLine(ctx)
	if errors.Is(err, io.EOF) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return line == expected, nil
}
func (r *terminalReviewer) Review(ctx context.Context, op apt.Operation) (bool, error) {
	detail := fmt.Sprintf("Review %s %s:%s version %s -> %s",
		op.Kind, op.Package, op.Architecture, display(op.OldVersion), display(op.NewVersion))
	return r.answer(ctx, "review "+op.Kind+" "+op.Package+":"+op.Architecture, detail)
}
func (r *terminalReviewer) ApproveRemoval(ctx context.Context, op apt.Operation) (bool, error) {
	if !op.Installed.Known || op.Installed.Essential || op.Installed.Protected {
		return false, errors.New("unsafe removal forbidden; no approval bypass")
	}
	detail := fmt.Sprintf("Remove %s:%s exact installed version %s",
		op.Package, op.Architecture, display(op.OldVersion))
	return r.answer(ctx, "remove "+op.Package+":"+op.Architecture, detail)
}
func (r *terminalReviewer) Confirm(ctx context.Context, _ apt.Preview) (bool, error) {
	return r.answer(ctx, "apply", "Fresh general transaction confirmation (does not authorize removals). Native APT confirmation follows.")
}

// Exactly one byte per Read: do not hide pre-fetched answers from native APT.
// Production uses readiness-polled terminal reads, never an arbitrary blocking
// Reader or a goroutine. The generic scalar helper permits deterministic fixtures.
func readScalarLine(ctx context.Context, r io.Reader) (string, error) {
	var b strings.Builder
	var one [1]byte
	for b.Len() <= 256 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := r.Read(one[:])
		if n > 0 {
			if one[0] == '\n' {
				return b.String(), nil
			}
			if one[0] < 32 || one[0] > 126 {
				return "", io.EOF
			}
			b.WriteByte(one[0])
		}
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "", io.EOF
		}
	}
	return "", io.EOF
}
