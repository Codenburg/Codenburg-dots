//go:build linux

package apt

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func protocolFixture(hooks []string) []byte {
	var b strings.Builder
	b.WriteString("VERSION 3\nBinary=apt-get\n")
	for _, line := range strings.Split(strings.TrimSuffix(executionConfig(hooks), "\n"), "\n") {
		if strings.HasPrefix(line, "#clear ") {
			continue
		}
		cfg, err := parseExecutionDump(line + "\n")
		if err != nil {
			panic(err)
		}
		for key, values := range cfg {
			for _, value := range values {
				b.WriteString(encodeConfig(key) + "=" + encodeConfig(value) + "\n")
			}
		}
	}
	b.WriteString("\n" + hookInstallRows)
	return []byte(b.String())
}

func TestTransportPrivateBoundedLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name   string
		length uint32
		body   []byte
	}{
		{name: "oversized frame", length: maxCommandOutput + 1},
		{name: "empty frame", length: 0},
		{name: "incomplete frame", length: 100, body: []byte("short")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport, err := newGuardTransport(context.Background(), New(hookRunner()), hookInstallPreview())
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := transport.close(); err != nil {
					t.Error(err)
				}
			}()
			if !validGuardPath(transport.path) {
				t.Fatal("unsafe generated hook path")
			}
			info, err := os.Stat(transport.dir)
			if err != nil || info.Mode().Perm() != 0700 {
				t.Fatal("directory not private", err)
			}
			info, err = os.Stat(transport.path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("socket not private", err)
			}
			// A private direct binding exercises frame limits independently of the real
			// descendant proof already exercised by fake apt's subprocess guard tests.
			if err := transport.bind(os.Getpid()); err != nil {
				t.Fatal(err)
			}
			conn, err := net.Dial("unix", transport.path)
			if err != nil {
				t.Fatal(err)
			}
			if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			var header [4]byte
			binary.BigEndian.PutUint32(header[:], tc.length)
			if _, err := io.Copy(conn, io.MultiReader(bytes.NewReader(header[:]), bytes.NewReader(tc.body))); err != nil {
				t.Fatal(err)
			}
			if err := conn.(*net.UnixConn).CloseWrite(); err != nil {
				t.Fatal(err)
			}
			var ack [1]byte
			if _, err := io.ReadFull(conn, ack[:]); err != nil || ack[0] != 0 {
				t.Fatalf("bad frame accepted: %v %v", ack, err)
			}
			if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
			if err := transport.close(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-transport.done:
			default:
				t.Fatal("server goroutine remains")
			}
			if _, err := os.Stat(transport.dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("private directory remains")
			}
			if err := transport.result(); err == nil {
				t.Fatal("bad frame granted evidence")
			}
		})
	}
}

func TestTransportNativeProfileCannotBeOmitted(t *testing.T) {
	transport, err := newGuardTransport(context.Background(), New(hookRunner()), hookInstallPreview())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := transport.close(); err != nil {
			t.Error(err)
		}
	}()
	transport.hooks = []string{"/proc/123/exe --internal-apt-guard " + transport.path}
	if err := transport.bind(os.Getpid()); err != nil {
		t.Fatal(err)
	}
	if err := guardClient(transport.path, bytes.NewReader(protocolFixture(transport.hooks))); err == nil {
		t.Fatal("missing native profile check granted authorization")
	}
	if err := transport.close(); err != nil {
		t.Fatal(err)
	}
	if err := transport.result(); err == nil || !strings.Contains(err.Error(), "missing native host profile validation") {
		t.Fatalf("native profile failure was not retained: %v", err)
	}
}

func TestTransportForeignAndExpiredOrigin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAPTExecutionHelper$", "--", "apt-timeout", "unused", "unused")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := cmd.Wait(); err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Error(err)
			}
		}
	}()
	transport, err := newGuardTransport(ctx, New(hookRunner()), hookInstallPreview())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := transport.close(); err != nil {
			t.Error(err)
		}
	}()
	transport.hooks = []string{"/proc/123/exe --internal-apt-guard " + transport.path}
	if err := transport.bind(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	// Even the same UID and same executable cannot connect from the coordinator:
	// the peer must descend from this exact live apt child.
	if err := guardClient(transport.path, bytes.NewReader(protocolFixture(transport.hooks))); err == nil {
		t.Fatal("foreign same-user peer authorized")
	}
	start, _, err := processIdentity(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkGuardOrigin(os.Getpid(), cmd.Process.Pid, start+"changed"); err == nil {
		t.Fatal("expired/reused invocation accepted")
	}
	if err := transport.close(); err != nil {
		t.Fatal(err)
	}
	if err := guardClient(transport.path, bytes.NewReader([]byte("VERSION 3\n\n"))); err == nil {
		t.Fatal("closed invocation reused")
	}
}

func TestEffectiveNativeConfiguration(t *testing.T) {
	hooks := []string{"/proc/123/exe --internal-apt-guard /tmp/cdots-123/guard", preconfigureHook}
	data := protocolFixture(hooks)
	if err := checkEffectiveHookConfig(data, hooks); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"purge enabled", bytes.Replace(data, []byte("apt::get::purge=false"), []byte("apt::get::purge=true"), 1)},
		{"changed protocol", bytes.Replace(data, []byte("::version=3"), []byte("::version=2"), 1)},
		{"changed fd", bytes.Replace(data, []byte("::infofd=0"), []byte("::infofd=1"), 1)},
		{"extra preinvoke", bytes.Replace(data, []byte("Binary=apt-get\n"), []byte("Binary=apt-get\ndpkg::pre-invoke::=malicious\n"), 1)},
		{"missing native binary scope", bytes.Replace(data, []byte("Binary=apt-get\n"), nil, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkEffectiveHookConfig(tc.data, hooks); err == nil {
				t.Fatal("unsafe effective config accepted")
			}
		})
	}
	for _, path := range []string{"/tmp/cdots-../guard", "/tmp/cdots-1;touch/guard", "/tmp/cdots-1/sub/guard", "/home/user/guard"} {
		if validGuardPath(path) {
			t.Fatal("unsafe hook argument", path)
		}
	}
}

// Debian APT 2.6.1 dpkgpm.cc:1210/1309/2008: Use-Pty=false
// makes master=-1/slave=NULL, so SetupSlavePtyMagic returns before setsid
// in the ExecFork child. fileutl.cc:898 ExecFork preserves group/session.
// The hook must prove that constrained profile, not infer it from our mocks.
func TestEffectiveNoPTYProfile(t *testing.T) {
	hooks := []string{"/proc/123/exe --internal-apt-guard /tmp/cdots-123/guard"}
	base := bytes.ReplaceAll(protocolFixture(hooks), []byte("dpkg::use-pty=false\n"), nil)
	with := func(value string) []byte {
		return bytes.Replace(base, []byte("Binary=apt-get\n"), []byte("Binary=apt-get\ndpkg::use-pty="+value+"\n"), 1)
	}
	for _, tc := range []struct {
		name     string
		data     []byte
		accepted bool
	}{
		{name: "missing", data: base},
		{name: "true", data: with("true")},
		{name: "conflicting", data: bytes.Replace(with("false"), []byte("dpkg::use-pty=false\n"), []byte("dpkg::use-pty=false\ndpkg::use-pty=true\n"), 1)},
		{name: "explicit false", data: with("false"), accepted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkEffectiveHookConfig(tc.data, hooks)
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%v err=%v", tc.accepted, err)
			}
		})
	}
}

func TestExecutionConfigurationDoesNotPersistCredentials(t *testing.T) {
	cfg, err := parseExecutionDump("Acquire::http::Proxy \"http://private-credential@example\";\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := checkExecutionConfig(cfg, false); err != nil {
		t.Fatal(err)
	}
	output := executionConfig([]string{"/proc/123/exe --internal-apt-guard /tmp/cdots-123/guard"})
	if strings.Contains(output, "private-credential") {
		t.Fatal("credential serialized")
	}
	if strings.Contains(output, "Assume-Yes \"true\"") || strings.Contains(output, "--force") {
		t.Fatal("unsafe generated config")
	}
}

func TestExecutorClearsInheritedLockEvidence(t *testing.T) {
	t.Setenv("DPKG_FRONTEND_LOCKED", "true")
	t.Setenv("APT_HOOK_INFO_FD", "0")
	marker := filepath.Join(t.TempDir(), "dpkg")
	e, _, _ := fixtureExecutor(t, "apt-nolock", marker, io.Discard, io.Discard)
	if err := e.Execute(context.Background(), hookInstallPreview()); err == nil {
		t.Fatal("stale inherited evidence authorized fake dpkg")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("guard bypassed by inherited environment")
	}
}
