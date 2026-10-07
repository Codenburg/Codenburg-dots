package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeSystem struct{}

func (fakeSystem) Detect() (SystemInfo, error) {
	return SystemInfo{ID: "ubuntu", Name: "Ubuntu", Architecture: "amd64", RawArchitecture: "x86_64", Supported: true, APTAvailable: true}, nil
}

type fakePackages struct{ err error }

func (f fakePackages) Inspect(context.Context, string) (PackageInfo, error) {
	return PackageInfo{Status: "current", InstalledVersion: "1"}, f.err
}

func TestRunSystemAndPackage(t *testing.T) {
	for _, args := range [][]string{nil, {"system"}} {
		var out, stderr bytes.Buffer
		if err := Run(args, &out, &stderr, fakeSystem{}, fakePackages{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Ubuntu") {
			t.Fatalf("missing system output: %q", out.String())
		}
	}
	var out, stderr bytes.Buffer
	if err := Run([]string{"package", "vim"}, &out, &stderr, fakeSystem{}, fakePackages{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "current") {
		t.Fatalf("missing package status: %q", out.String())
	}
}

func TestRunReportsUsageAndInspectionErrors(t *testing.T) {
	var out, stderr bytes.Buffer
	if err := Run([]string{"package"}, &out, &stderr, fakeSystem{}, fakePackages{}); err == nil {
		t.Fatal("missing operand accepted")
	}
	if err := Run([]string{"package", "vim"}, &out, &stderr, fakePackagesErr{errors.New("apt failed")}, fakePackages{err: errors.New("dpkg failed")}); err == nil {
		t.Fatal("inspection failure hidden")
	}
}

func TestRegressionRunReachesInspectorError(t *testing.T) {
	var out, stderr bytes.Buffer
	failure := errors.New("injected package query failure")
	err := Run([]string{"package", "vim"}, &out, &stderr, fakeSystem{}, fakePackages{err: failure})
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "inspect package vim") {
		t.Fatalf("inspector error not propagated: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("success output written for failed inspection: %q", out.String())
	}
}

type fakePackagesErr struct{ err error }

func (f fakePackagesErr) Detect() (SystemInfo, error) { return SystemInfo{}, f.err }
