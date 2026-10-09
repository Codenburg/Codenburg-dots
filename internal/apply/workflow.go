// Package apply coordinates injected APT review and execution. It contains no
// OS executor, elevation, CLI prompting, or persistent authorization.
package apply

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Codenburg/Codenburg-dots/internal/packages"
	"github.com/Codenburg/Codenburg-dots/internal/providers/apt"
)

var (
	ErrFreshReviewRequired = errors.New("state or transaction changed; fresh review required")
	ErrRejected            = errors.New("transaction approval rejected")
	ErrVerification        = errors.New("approved package state not verified")
)

type Backend interface {
	InspectApply(context.Context, string) (packages.Info, error)
	InspectApplyState(context.Context, string) (apt.ApplyState, error)
	Simulate(context.Context, []string) (apt.Preview, error)
	ValidateApply(context.Context, apt.Preview) error
}

// Reviewer must reject noninteractive use. Render is called before any question.
// Review is specific to each upgrade, high-risk or critical nonremoval change;
// removal approval and fresh general confirmation are separate boundaries.
// Every callback receives an isolated copy, never execution authority.
type Reviewer interface {
	Render(context.Context, apt.Preview) error
	Review(context.Context, apt.Operation) (bool, error)
	ApproveRemoval(context.Context, apt.Operation) (bool, error)
	Confirm(context.Context, apt.Preview) (bool, error)
}
type Executor interface {
	Execute(context.Context, apt.Preview) error
}

type Status string

const (
	NoAction     Status = "no-action"
	Success      Status = "success"
	Partial      Status = "partial"
	Failure      Status = "failure"
	Cancellation Status = "cancellation"
)

type Verification struct {
	Package         string
	ExpectedVersion string
	Removal         bool
	Observed        packages.Info
	Verified        bool
	Err             error
}
type Result struct {
	Status       Status
	Preview      apt.Preview
	Verification []Verification
	Executed     bool
	Err          error
}

type Workflow struct {
	Backend  Backend
	Reviewer Reviewer
	Executor Executor
	// VerificationTimeout bounds independent post-execution inspection even after
	// interruption. Zero uses 30 seconds; it never detaches execution itself.
	VerificationTimeout time.Duration
}

func (w Workflow) Run(ctx context.Context, names []string) (Result, error) {
	result := Result{Status: Failure, Verification: []Verification{}}
	finish := func(err error) (Result, error) {
		result.Err = err
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
			(!result.Executed && errors.Is(err, ErrRejected)) {
			result.Status = Cancellation
		}
		return result, err
	}
	if w.Backend == nil {
		return finish(errors.New("missing apply backend"))
	}
	names = slices.Clone(names)
	slices.Sort(names)
	if len(names) == 0 {
		return finish(errors.New("empty requested packages"))
	}
	for i, name := range names {
		if name == "" || (i > 0 && name == names[i-1]) {
			return finish(errors.New("empty or duplicate requested package"))
		}
	}
	before, missing, err := w.inspect(ctx, names)
	if err != nil {
		return finish(err)
	}
	if len(missing) == 0 {
		result.Status = NoAction
		return finish(nil)
	}
	preview, err := w.Backend.Simulate(ctx, slices.Clone(missing))
	result.Preview = clonePreview(preview)
	if err != nil {
		return finish(fmt.Errorf("simulate: %w", err))
	}
	preview = clonePreview(preview)
	if !sameRequests(preview.Requests, missing) {
		return finish(errors.New("simulation requests contradict missing operands"))
	}
	if err = w.Backend.ValidateApply(ctx, clonePreview(preview)); err != nil {
		return finish(fmt.Errorf("validate apply: %w", err))
	}
	if err = bindRequested(before, preview); err != nil {
		return finish(err)
	}
	if w.Reviewer == nil || w.Executor == nil {
		return finish(errors.New("real operation requires injected reviewer and executor"))
	}
	if err = w.Reviewer.Render(ctx, clonePreview(preview)); err != nil {
		return finish(fmt.Errorf("render review: %w", err))
	}
	for _, op := range preview.Operations {
		var approved bool
		if op.Kind == "removal" {
			approved, err = w.Reviewer.ApproveRemoval(ctx, cloneOperation(op))
		} else if needsReview(op) {
			approved, err = w.Reviewer.Review(ctx, cloneOperation(op))
		} else {
			continue
		}
		if err != nil {
			return finish(fmt.Errorf("review %s: %w", op.Package, err))
		}
		if !approved {
			return finish(fmt.Errorf("%s: %w", op.Package, ErrRejected))
		}
	}
	approved, err := w.Reviewer.Confirm(ctx, clonePreview(preview))
	if err != nil {
		return finish(fmt.Errorf("confirm: %w", err))
	}
	if !approved {
		return finish(ErrRejected)
	}
	if err = ctx.Err(); err != nil {
		return finish(err)
	}
	after, newMissing, err := w.inspect(ctx, names)
	if err != nil {
		return finish(errors.Join(ErrFreshReviewRequired, err))
	}
	if !slices.Equal(before, after) || !slices.Equal(missing, newMissing) {
		return finish(ErrFreshReviewRequired)
	}
	fresh, err := w.Backend.Simulate(ctx, slices.Clone(newMissing))
	if err != nil {
		return finish(errors.Join(ErrFreshReviewRequired, err))
	}
	fresh = clonePreview(fresh)
	if err = w.Backend.ValidateApply(ctx, clonePreview(fresh)); err != nil {
		return finish(errors.Join(ErrFreshReviewRequired, err))
	}
	if err = bindRequested(after, fresh); err != nil {
		return finish(errors.Join(ErrFreshReviewRequired, err))
	}
	if !apt.SameTransaction(preview, fresh) {
		return finish(ErrFreshReviewRequired)
	}
	if err = ctx.Err(); err != nil {
		return finish(err)
	}
	result.Executed = true
	execErr := w.Executor.Execute(ctx, clonePreview(preview))
	timeout := w.VerificationTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	verifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	result.Verification = w.verify(verifyCtx, names, before, preview)
	verified := 0
	issues := []error{execErr, ctx.Err()}
	for _, v := range result.Verification {
		if v.Verified {
			verified++
		} else {
			issues = append(issues, fmt.Errorf("verify %s: %w", v.Package, errors.Join(ErrVerification, v.Err)))
		}
	}
	err = errors.Join(issues...)
	switch {
	case err == nil:
		result.Status = Success
	case verified > 0:
		result.Status = Partial
	default:
		result.Status = Failure
	}
	return finish(err)
}

func (w Workflow) inspect(ctx context.Context, names []string) ([]apt.ApplyState, []string, error) {
	state := make([]apt.ApplyState, 0, len(names))
	missing := []string{}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return state, missing, err
		}
		snapshot, err := w.Backend.InspectApplyState(ctx, name)
		if err != nil {
			return state, missing, fmt.Errorf("inspect %s: %w", name, err)
		}
		info := snapshot.Info
		if err = validateState(name, snapshot); err != nil {
			return state, missing, err
		}
		if info.Name != name {
			return state, missing, errors.New("inspection identity mismatch")
		}
		switch info.Status {
		case packages.Current, packages.UpdateAvailable:
			if info.InstalledVersion == "" || info.CandidateVersion == "" {
				return state, missing, errors.New("incomplete installed inspection")
			}
		case packages.Available:
			if info.InstalledVersion != "" || info.CandidateVersion == "" {
				return state, missing, errors.New("contradictory available inspection")
			}
			missing = append(missing, name)
		default:
			return state, missing, fmt.Errorf("unavailable or unknown package %s", name)
		}
		state = append(state, snapshot)
	}
	return state, missing, ctx.Err()
}

func bindRequested(state []apt.ApplyState, preview apt.Preview) error {
	for _, snapshot := range state {
		base, _, _ := strings.Cut(snapshot.Info.Name, ":")
		if snapshot.Info.InstalledVersion != "" {
			for _, op := range preview.Operations {
				if op.Package != base {
					continue
				}
				if op.Kind == "removal" || op.Kind == "install" {
					return errors.New("transaction contradicts satisfied requested state")
				}
				if op.Installed != snapshot.Installed {
					return errors.New("transaction contradicts satisfied installed evidence")
				}
				if op.Kind == "upgrade" && op.Candidate != snapshot.Candidate {
					return errors.New("transaction contradicts satisfied candidate evidence")
				}
			}
			continue
		}
		found := false
		for _, op := range preview.Operations {
			if op.Package == base && op.Kind == "install" {
				found = true
				if op.Candidate != snapshot.Candidate || op.Installed != snapshot.Installed {
					return errors.New("simulation contradicts requested safety evidence")
				}
			}
		}
		if !found {
			return errors.New("requested install missing from preview")
		}
	}
	return nil
}

func validateState(name string, state apt.ApplyState) error {
	info := state.Info
	base, arch, _ := strings.Cut(name, ":")
	bound := func(e apt.PackageEvidence, version string) bool {
		return e.Known && e.Package == base && e.Version == version && e.Architecture != "" && (arch == "" || e.Architecture == arch)
	}
	if info.InstalledVersion != "" {
		e := state.Installed
		if !bound(e, info.InstalledVersion) {
			return errors.New("unbound requested installed evidence")
		}
		if (e.Status != "install ok installed" && e.Status != "hold ok installed") || e.Held != (e.Status == "hold ok installed") {
			return errors.New("partial or contradictory requested installed status")
		}
	} else if state.Installed != (apt.PackageEvidence{}) {
		return errors.New("contradictory requested absence")
	}
	if info.CandidateVersion != "" {
		e := state.Candidate
		if !bound(e, info.CandidateVersion) || e.Status != "" || e.Held {
			return errors.New("unbound requested candidate evidence")
		}
	}
	return nil
}

func needsReview(op apt.Operation) bool {
	critical := op.Installed.Essential || op.Installed.Protected || op.Candidate.Essential || op.Candidate.Protected
	return op.Kind == "upgrade" || op.Kind == "configuration" || critical || op.Risk == "high" || op.Risk == "review-required"
}
func sameRequests(a, b []string) bool {
	a = slices.Clone(a)
	b = slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
func cloneOperation(op apt.Operation) apt.Operation {
	op.Uncertainty = slices.Clone(op.Uncertainty)
	return op
}
func clonePreview(v apt.Preview) apt.Preview {
	v.Requests = slices.Clone(v.Requests)
	v.Operations = slices.Clone(v.Operations)
	for i := range v.Operations {
		v.Operations[i] = cloneOperation(v.Operations[i])
	}
	v.Metadata = slices.Clone(v.Metadata)
	v.Warnings = slices.Clone(v.Warnings)
	v.ReviewRequirements = slices.Clone(v.ReviewRequirements)
	return v
}

func (w Workflow) verify(ctx context.Context, names []string, before []apt.ApplyState, preview apt.Preview) []Verification {
	expected := map[string]Verification{}
	// Satisfied requested packages must also remain present at their approved version.
	for i, name := range names {
		expected[name] = Verification{Package: name, ExpectedVersion: before[i].Info.InstalledVersion}
	}
	for _, op := range preview.Operations {
		identity := op.Package + ":" + op.Architecture
		// Replace an unqualified requested identity, rather than reporting it twice.
		for name := range expected {
			base, arch, _ := strings.Cut(name, ":")
			if base == op.Package && (arch == "" || arch == op.Architecture) {
				delete(expected, name)
			}
		}
		expected[identity] = Verification{Package: identity, ExpectedVersion: op.NewVersion, Removal: op.Kind == "removal"}
	}
	keys := make([]string, 0, len(expected))
	for name := range expected {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	result := make([]Verification, 0, len(keys))
	for _, name := range keys {
		v := expected[name]
		v.Observed, v.Err = w.Backend.InspectApply(ctx, name)
		v.Err = errors.Join(v.Err, ctx.Err())
		reliable := v.Err == nil && v.Observed.Name == name
		present := v.Observed.Status == packages.Current || v.Observed.Status == packages.UpdateAvailable
		absent := v.Observed.Status == packages.Available || v.Observed.Status == packages.Unavailable
		if v.Removal {
			v.Verified = reliable && absent && v.Observed.InstalledVersion == ""
		} else {
			v.Verified = reliable && present && v.ExpectedVersion != "" && v.Observed.InstalledVersion == v.ExpectedVersion
		}
		result = append(result, v)
	}
	return result
}
