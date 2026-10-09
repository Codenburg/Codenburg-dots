package apply

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Codenburg/Codenburg-dots/internal/packages"
	"github.com/Codenburg/Codenburg-dots/internal/providers/apt"
)

type fakeBackend struct {
	installed                bool
	simulations, inspections int
	preview                  apt.Preview
	inspect                  func(context.Context, string, int) (packages.Info, error)
	simulate                 func(int, *apt.Preview) error
	stateChange              func(int, *apt.ApplyState)
}

func (b *fakeBackend) InspectApply(ctx context.Context, name string) (packages.Info, error) {
	b.inspections++
	if b.inspect != nil {
		return b.inspect(ctx, name, b.inspections)
	}
	if b.installed {
		return packages.Info{Name: name, Status: packages.Current, InstalledVersion: "1", CandidateVersion: "1"}, nil
	}
	return packages.Info{Name: name, Status: packages.Available, CandidateVersion: "1"}, nil
}
func (b *fakeBackend) Simulate(_ context.Context, names []string) (apt.Preview, error) {
	b.simulations++
	v := clonePreview(b.preview)
	v.Requests = append([]string{}, names...)
	if b.simulate != nil {
		if err := b.simulate(b.simulations, &v); err != nil {
			return v, err
		}
	}
	return v, nil
}
func (b *fakeBackend) InspectApplyState(ctx context.Context, name string) (apt.ApplyState, error) {
	info, err := b.InspectApply(ctx, name)
	state := apt.ApplyState{Info: info}
	base, arch, _ := strings.Cut(name, ":")
	if arch == "" {
		arch = "amd64"
	}
	if info.InstalledVersion != "" {
		state.Installed = apt.PackageEvidence{Known: true, Package: base, Architecture: arch, Version: info.InstalledVersion, Status: "install ok installed"}
	}
	if info.CandidateVersion != "" {
		state.Candidate = apt.PackageEvidence{Known: true, Package: base, Architecture: arch, Version: info.CandidateVersion}
	}
	if base == "demo" && len(b.preview.Operations) > 0 {
		state.Candidate.Essential = b.preview.Operations[0].Candidate.Essential
		state.Candidate.Protected = b.preview.Operations[0].Candidate.Protected
	}
	if b.stateChange != nil {
		b.stateChange(b.inspections, &state)
	}
	return state, err
}

// Validation is the actual strict provider validator; only its dpkg runner is fake.
func (b *fakeBackend) ValidateApply(ctx context.Context, v apt.Preview) error {
	return apt.New(compareRunner{}).ValidateApply(ctx, v)
}

type compareRunner struct{}

func (compareRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	if name != "dpkg" || len(args) != 4 || args[0] != "--compare-versions" {
		return "", "", errors.New("unexpected command")
	}
	if args[1] == "2" && args[2] == "gt" && args[3] == "1" {
		return "", "", nil
	}
	return "", "", falseComparison{}
}

type falseComparison struct{}

func (falseComparison) Error() string { return "false comparison" }
func (falseComparison) ExitCode() int { return 1 }

type fakeReviewer struct {
	events []string
	reject string
	err    error
	mutate bool
}

func (r *fakeReviewer) Render(_ context.Context, v apt.Preview) error {
	r.events = append(r.events, "render")
	if r.mutate {
		v.Requests[0] = "evil"
		v.Operations[0].NewVersion = "99"
		v.Operations[0].Candidate.Protected = true
		v.Operations = append(v.Operations, apt.Operation{})
	}
	if r.reject == "render" {
		return r.err
	}
	return nil
}
func (r *fakeReviewer) answer(event string) (bool, error) {
	r.events = append(r.events, event)
	if r.reject == event {
		return false, r.err
	}
	return true, nil
}
func (r *fakeReviewer) Review(_ context.Context, op apt.Operation) (bool, error) {
	return r.answer("review:" + op.Package)
}
func (r *fakeReviewer) ApproveRemoval(_ context.Context, op apt.Operation) (bool, error) {
	return r.answer("remove:" + op.Package)
}
func (r *fakeReviewer) Confirm(_ context.Context, v apt.Preview) (bool, error) {
	if r.mutate {
		v.Operations[0].Architecture = "evil"
	}
	return r.answer("confirm")
}

type fakeExecutor struct {
	backend *fakeBackend
	calls   int
	err     error
	run     func(context.Context, apt.Preview) error
}

func (e *fakeExecutor) Execute(ctx context.Context, v apt.Preview) error {
	e.calls++
	if e.run != nil {
		return e.run(ctx, v)
	}
	if e.err == nil {
		e.backend.installed = true
	}
	return e.err
}
func fixture() (Workflow, *fakeBackend, *fakeReviewer, *fakeExecutor) {
	b := &fakeBackend{preview: apt.Preview{Operations: []apt.Operation{{Kind: "install", Package: "demo", Architecture: "amd64", NewVersion: "1", Requested: true, Risk: "normal", Candidate: apt.PackageEvidence{Known: true, Package: "demo", Architecture: "amd64", Version: "1"}}}}}
	r := &fakeReviewer{}
	e := &fakeExecutor{backend: b}
	return Workflow{Backend: b, Reviewer: r, Executor: e}, b, r, e
}
func indirect(kind, name string) apt.Operation {
	op := apt.Operation{Kind: kind, Package: name, Architecture: "amd64", OldVersion: "1", NewVersion: "2", Risk: "review-required", Installed: apt.PackageEvidence{Known: true, Package: name, Architecture: "amd64", Version: "1", Status: "install ok installed"}, Candidate: apt.PackageEvidence{Known: true, Package: name, Architecture: "amd64", Version: "2"}}
	if kind == "removal" {
		op.NewVersion = ""
		op.Candidate = apt.PackageEvidence{}
	}
	return op
}

func TestRunRefusalIsCancellationOnlyBeforeExecution(t *testing.T) {
	w, _, r, e := fixture()
	r.reject = "confirm"
	result, err := w.Run(context.Background(), []string{"demo"})
	if result.Status != Cancellation || !errors.Is(err, ErrRejected) || e.calls != 0 {
		t.Fatalf("refusal: %+v %v", result, err)
	}
	w, _, _, e = fixture()
	e.err = ErrRejected
	result, err = w.Run(context.Background(), []string{"demo"})
	if result.Status != Failure || !result.Executed {
		t.Fatalf("executor rejection mislabeled: %+v %v", result, err)
	}
}

func TestRunNoActionAndInstall(t *testing.T) {
	for _, installed := range []bool{true, false} {
		name := "install"
		if installed {
			name = "noop"
		}
		t.Run(name, func(t *testing.T) {
			w, b, r, e := fixture()
			b.installed = installed
			if installed {
				w.Reviewer = nil
				w.Executor = nil
			}
			result, err := w.Run(context.Background(), []string{"demo"})
			if err != nil {
				t.Fatal(err)
			}
			if installed {
				if result.Status != NoAction || e.calls != 0 || b.simulations != 0 || len(r.events) != 0 {
					t.Fatalf("unexpected noop: %+v", result)
				}
				return
			}
			if result.Status != Success || e.calls != 1 || b.simulations != 2 || len(result.Verification) != 1 || !result.Verification[0].Verified {
				t.Fatalf("unexpected install: %+v", result)
			}
			if strings.Join(r.events, ",") != "render,confirm" {
				t.Fatal(r.events)
			}
		})
	}
}
func TestRunFiltersSatisfiedRequests(t *testing.T) {
	w, b, _, e := fixture()
	b.inspect = func(_ context.Context, name string, _ int) (packages.Info, error) {
		if name == "satisfied" {
			return packages.Info{Name: name, Status: packages.UpdateAvailable, InstalledVersion: "old", CandidateVersion: "new"}, nil
		}
		version := ""
		status := packages.Available
		if e.calls > 0 {
			version = "1"
			status = packages.Current
		}
		return packages.Info{Name: name, Status: status, InstalledVersion: version, CandidateVersion: "1"}, nil
	}
	result, err := w.Run(context.Background(), []string{"satisfied", "demo"})
	if err != nil || result.Status != Success || strings.Join(result.Preview.Requests, ",") != "demo" || len(result.Verification) != 2 {
		t.Fatalf("%+v %v", result, err)
	}
}
func TestRunCannotRemoveSatisfiedRequestedPackage(t *testing.T) {
	w, b, _, e := fixture()
	b.preview.Operations = append(b.preview.Operations, indirect("removal", "satisfied"))
	b.inspect = func(_ context.Context, name string, _ int) (packages.Info, error) {
		if name == "satisfied" {
			return packages.Info{Name: name, Status: packages.Current, InstalledVersion: "1", CandidateVersion: "1"}, nil
		}
		if e.calls > 0 {
			return packages.Info{Name: name, Status: packages.Current, InstalledVersion: "1", CandidateVersion: "1"}, nil
		}
		return packages.Info{Name: name, Status: packages.Available, CandidateVersion: "1"}, nil
	}
	_, err := w.Run(context.Background(), []string{"demo", "satisfied"})
	if err == nil || e.calls != 0 {
		t.Fatalf("requested desired-present package authorized for removal: err=%v execute=%d", err, e.calls)
	}
}

func TestRunReviewBoundaries(t *testing.T) {
	for _, reject := range []string{"", "confirm", "review:libdemo", "remove:obsolete", "remove:other", "render"} {
		t.Run("reject="+reject, func(t *testing.T) {
			w, b, r, e := fixture()
			r.reject = reject
			if reject == "render" {
				r.err = errors.New("noninteractive reviewer")
			}
			b.preview.Operations = append(b.preview.Operations, indirect("upgrade", "libdemo"), indirect("removal", "obsolete"), indirect("removal", "other"))
			e.run = func(_ context.Context, v apt.Preview) error {
				b.inspect = func(_ context.Context, name string, _ int) (packages.Info, error) {
					info := packages.Info{Name: name, Status: packages.Current, InstalledVersion: "1", CandidateVersion: "1"}
					if strings.HasPrefix(name, "libdemo") {
						info.InstalledVersion = "2"
						info.CandidateVersion = "2"
					}
					if strings.HasPrefix(name, "obsolete") || strings.HasPrefix(name, "other") {
						info.Status = packages.Unavailable
						info.InstalledVersion = ""
						info.CandidateVersion = ""
					}
					return info, nil
				}
				return nil
			}
			result, err := w.Run(context.Background(), []string{"demo"})
			if reject != "" {
				if err == nil || e.calls != 0 {
					t.Fatalf("executed rejected transaction: %+v %v", result, err)
				}
				return
			}
			if err != nil || result.Status != Success || e.calls != 1 {
				t.Fatalf("%+v %v", result, err)
			}
			if strings.Join(r.events, ",") != "render,review:libdemo,remove:obsolete,remove:other,confirm" {
				t.Fatal(r.events)
			}
		})
	}
}
func TestRunSafetyRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*apt.Preview)
	}{
		{name: "unknown", change: func(v *apt.Preview) { v.Operations[0].Candidate.Known = false }},
		{name: "contradictory", change: func(v *apt.Preview) { v.Operations[0].Candidate.Version = "other" }},
		{name: "incomplete", change: func(v *apt.Preview) { v.Operations = nil }},
		{name: "essential removal", change: func(v *apt.Preview) {
			op := indirect("removal", "libdemo")
			op.Installed.Essential = true
			v.Operations = append(v.Operations, op)
		}},
		{name: "protected removal", change: func(v *apt.Preview) {
			op := indirect("removal", "libdemo")
			op.Installed.Protected = true
			v.Operations = append(v.Operations, op)
		}},
		{name: "hold", change: func(v *apt.Preview) {
			op := indirect("upgrade", "libdemo")
			op.Installed.Held = true
			v.Operations = append(v.Operations, op)
		}},
		{name: "downgrade", change: func(v *apt.Preview) {
			op := indirect("upgrade", "libdemo")
			op.NewVersion = "0"
			op.Candidate.Version = "0"
			v.Operations = append(v.Operations, op)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, b, r, e := fixture()
			tc.change(&b.preview)
			_, err := w.Run(context.Background(), []string{"demo"})
			if err == nil || e.calls != 0 || len(r.events) != 0 {
				t.Fatalf("err=%v execute=%d review=%v", err, e.calls, r.events)
			}
		})
	}
}
func TestRunCriticalSpecificReview(t *testing.T) {
	for _, flag := range []string{"essential", "protected", "high"} {
		t.Run(flag, func(t *testing.T) {
			w, b, r, e := fixture()
			switch flag {
			case "essential":
				b.preview.Operations[0].Candidate.Essential = true
			case "protected":
				b.preview.Operations[0].Candidate.Protected = true
			case "high":
				b.preview.Operations[0].Risk = "high"
			}
			r.reject = "review:demo"
			_, err := w.Run(context.Background(), []string{"demo"})
			if err == nil || e.calls != 0 || strings.Join(r.events, ",") != "render,review:demo" {
				t.Fatalf("%v %v", err, r.events)
			}
		})
	}
}
func TestRunDrift(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*apt.Preview)
	}{
		{name: "version", change: func(v *apt.Preview) { v.Operations[0].NewVersion = "2"; v.Operations[0].Candidate.Version = "2" }},
		{name: "architecture", change: func(v *apt.Preview) {
			v.Operations[0].Architecture = "all"
			v.Operations[0].Candidate.Architecture = "all"
		}},
		{name: "protection", change: func(v *apt.Preview) { v.Operations[0].Candidate.Protected = true }},
		{name: "installed metadata", change: func(v *apt.Preview) {
			op := indirect("upgrade", "libdemo")
			op.Installed.Essential = true
			v.Operations = append(v.Operations, op)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, b, _, e := fixture()
			b.simulate = func(n int, v *apt.Preview) error {
				if n == 2 {
					tc.change(v)
				}
				return nil
			}
			_, err := w.Run(context.Background(), []string{"demo"})
			if !errors.Is(err, ErrFreshReviewRequired) || e.calls != 0 {
				t.Fatalf("%v calls=%d", err, e.calls)
			}
		})
	}
	t.Run("requested state", func(t *testing.T) {
		w, b, _, e := fixture()
		b.inspect = func(_ context.Context, name string, n int) (packages.Info, error) {
			candidate := "1"
			if n > 1 {
				candidate = "2"
			}
			return packages.Info{Name: name, Status: packages.Available, CandidateVersion: candidate}, nil
		}
		_, err := w.Run(context.Background(), []string{"demo"})
		if !errors.Is(err, ErrFreshReviewRequired) || e.calls != 0 {
			t.Fatal(err)
		}
	})
}
func TestRunRequestedBoundStateAndDrift(t *testing.T) {
	for _, field := range []string{"known", "version", "architecture", "status", "holds", "protection"} {
		t.Run(field, func(t *testing.T) {
			w, b, _, e := fixture()
			b.stateChange = func(n int, state *apt.ApplyState) {
				if n < 2 {
					return
				}
				switch field {
				case "known":
					state.Candidate.Known = false
				case "version":
					state.Candidate.Version = "2"
				case "architecture":
					state.Candidate.Architecture = "all"
				case "status":
					state.Candidate.Status = "install ok installed"
				case "holds":
					state.Candidate.Held = true
				case "protection":
					state.Candidate.Protected = true
				}
			}
			_, err := w.Run(context.Background(), []string{"demo"})
			if !errors.Is(err, ErrFreshReviewRequired) || e.calls != 0 {
				t.Fatalf("%v %d", err, e.calls)
			}
		})
	}
	t.Run("unknown initial evidence", func(t *testing.T) {
		w, b, r, e := fixture()
		b.stateChange = func(_ int, state *apt.ApplyState) { state.Candidate.Known = false }
		_, err := w.Run(context.Background(), []string{"demo"})
		if err == nil || e.calls != 0 || len(r.events) != 0 {
			t.Fatal("unknown candidate permitted")
		}
	})
	t.Run("snapshot versus simulation", func(t *testing.T) {
		w, b, r, e := fixture()
		b.stateChange = func(_ int, state *apt.ApplyState) { state.Candidate.Protected = true }
		_, err := w.Run(context.Background(), []string{"demo"})
		if err == nil || e.calls != 0 || len(r.events) != 0 {
			t.Fatal("contradictory protection permitted")
		}
	})
	t.Run("satisfied protection drift", func(t *testing.T) {
		w, b, _, e := fixture()
		b.inspect = func(_ context.Context, name string, _ int) (packages.Info, error) {
			if name == "satisfied" {
				return packages.Info{Name: name, Status: packages.Current, InstalledVersion: "1", CandidateVersion: "1"}, nil
			}
			return packages.Info{Name: name, Status: packages.Available, CandidateVersion: "1"}, nil
		}
		b.stateChange = func(n int, state *apt.ApplyState) {
			if n > 2 && state.Info.Name == "satisfied" {
				state.Installed.Protected = true
			}
		}
		_, err := w.Run(context.Background(), []string{"demo", "satisfied"})
		if !errors.Is(err, ErrFreshReviewRequired) || e.calls != 0 {
			t.Fatal(err)
		}
	})
}

func TestRunRevalidationErrors(t *testing.T) {
	sentinel := errors.New("fresh backend error")
	for _, stage := range []string{"inspect", "simulate"} {
		t.Run(stage, func(t *testing.T) {
			w, b, _, e := fixture()
			if stage == "inspect" {
				b.inspect = func(_ context.Context, name string, n int) (packages.Info, error) {
					if n > 1 {
						return packages.Info{}, sentinel
					}
					return packages.Info{Name: name, Status: packages.Available, CandidateVersion: "1"}, nil
				}
			} else {
				b.simulate = func(n int, _ *apt.Preview) error {
					if n > 1 {
						return sentinel
					}
					return nil
				}
			}
			_, err := w.Run(context.Background(), []string{"demo"})
			if !errors.Is(err, ErrFreshReviewRequired) || !errors.Is(err, sentinel) || e.calls != 0 {
				t.Fatal(err)
			}
		})
	}
}

func TestRunReviewerErrorsAndFreshConfirmation(t *testing.T) {
	sentinel := errors.New("reviewer error")
	for _, stage := range []string{"confirm", "review:libdemo", "remove:obsolete"} {
		t.Run(stage, func(t *testing.T) {
			w, b, r, e := fixture()
			b.preview.Operations = append(b.preview.Operations, indirect("upgrade", "libdemo"), indirect("removal", "obsolete"))
			r.reject = stage
			r.err = sentinel
			_, err := w.Run(context.Background(), []string{"demo"})
			if !errors.Is(err, sentinel) || e.calls != 0 {
				t.Fatal(err)
			}
		})
	}
	w, b, r, e := fixture()
	for i := 0; i < 2; i++ {
		b.installed = false
		_, err := w.Run(context.Background(), []string{"demo"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if e.calls != 2 || strings.Join(r.events, ",") != "render,confirm,render,confirm" {
		t.Fatal("approval reused", r.events)
	}
}

func TestRunFreezesAuthorityAndIgnoresDiagnostics(t *testing.T) {
	w, b, r, e := fixture()
	r.mutate = true
	b.simulate = func(n int, v *apt.Preview) error {
		v.Simulation.Stdout = strings.Repeat("diagnostic", n)
		v.ReviewRequirements = []string{"prose"}
		return nil
	}
	e.run = func(_ context.Context, v apt.Preview) error {
		if v.Requests[0] != "demo" || v.Operations[0].NewVersion != "1" || v.Operations[0].Architecture != "amd64" {
			t.Fatal("review mutated authority")
		}
		v.Operations[0].NewVersion = "99"
		b.installed = true
		return nil
	}
	result, err := w.Run(context.Background(), []string{"demo"})
	if err != nil || result.Status != Success || result.Preview.Operations[0].NewVersion != "1" {
		t.Fatalf("%+v %v", result, err)
	}
}
func TestRunUnavailableAndInspectionError(t *testing.T) {
	sentinel := errors.New("inspection failure")
	for _, status := range []packages.Status{packages.Unavailable, packages.Unknown, "partial", "error"} {
		t.Run(string(status), func(t *testing.T) {
			w, b, r, e := fixture()
			b.inspect = func(_ context.Context, name string, _ int) (packages.Info, error) {
				if status == "error" {
					return packages.Info{}, sentinel
				}
				return packages.Info{Name: name, Status: status}, nil
			}
			_, err := w.Run(context.Background(), []string{"demo"})
			if err == nil || e.calls != 0 || b.simulations != 0 || len(r.events) != 0 {
				t.Fatal("invalid state proceeded")
			}
			if status == "error" && !errors.Is(err, sentinel) {
				t.Fatal("lost error chain")
			}
		})
	}
}
func TestRunExecutorAndPostVerificationFailures(t *testing.T) {
	privilege := errors.New("privilege denied")
	postErr := errors.New("post inspection unavailable")
	for _, tc := range []struct {
		name    string
		execErr error
		post    string
		want    Status
	}{
		{name: "privilege", execErr: privilege, want: Failure},
		{name: "exit0 missing", post: "missing", want: Failure},
		{name: "exit0 wrong version", post: "wrong", want: Failure},
		{name: "postverify error", post: "error", want: Failure},
		{name: "partial execution", execErr: privilege, post: "partial", want: Partial},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, b, _, e := fixture()
			if tc.post == "partial" {
				b.preview.Operations = append(b.preview.Operations, indirect("upgrade", "libdemo"))
			}
			e.run = func(_ context.Context, _ apt.Preview) error {
				b.inspect = func(_ context.Context, name string, _ int) (packages.Info, error) {
					if tc.post == "error" {
						return packages.Info{}, postErr
					}
					if tc.post == "wrong" {
						return packages.Info{Name: name, Status: packages.Current, InstalledVersion: "99", CandidateVersion: "99"}, nil
					}
					if tc.post == "partial" && strings.HasPrefix(name, "demo") {
						return packages.Info{Name: name, Status: packages.Current, InstalledVersion: "1", CandidateVersion: "1"}, nil
					}
					return packages.Info{Name: name, Status: packages.Available, CandidateVersion: "1"}, nil
				}
				return tc.execErr
			}
			result, err := w.Run(context.Background(), []string{"demo"})
			if err == nil || result.Status != tc.want || e.calls != 1 || len(result.Verification) == 0 {
				t.Fatalf("%+v %v", result, err)
			}
			if tc.execErr != nil && !errors.Is(err, tc.execErr) {
				t.Fatal("lost executor chain")
			}
			if tc.post == "error" && !errors.Is(err, postErr) {
				t.Fatal("lost verification chain")
			}
		})
	}
}
func TestRunCancellationAlwaysVerifiesAfterExecute(t *testing.T) {
	w, b, _, e := fixture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.run = func(_ context.Context, _ apt.Preview) error {
		cancel()
		b.inspect = func(ctx context.Context, name string, _ int) (packages.Info, error) {
			if ctx.Err() != nil {
				t.Fatal("verification inherited cancellation")
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("verification unbounded")
			}
			return packages.Info{Name: name, Status: packages.Available, CandidateVersion: "1"}, nil
		}
		return context.Canceled
	}
	result, err := w.Run(ctx, []string{"demo"})
	if result.Status != Cancellation || !errors.Is(err, context.Canceled) || len(result.Verification) != 1 || e.calls != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	t.Run("before execute", func(t *testing.T) {
		w, _, _, e := fixture()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result, err := w.Run(ctx, []string{"demo"})
		if result.Status != Cancellation || !errors.Is(err, context.Canceled) || e.calls != 0 {
			t.Fatal(err)
		}
	})
}
