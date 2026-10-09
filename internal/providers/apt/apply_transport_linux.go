//go:build linux

package apt

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type guardTransport struct {
	dir, path   string
	listener    *net.UnixListener
	ctx         context.Context
	cancel      context.CancelFunc
	provider    *Provider
	preview     Preview
	hooks       []string
	checkHost   func() error
	ready, done chan struct{}
	mu          sync.Mutex
	closeOnce   sync.Once
	closeErr    error
	pid         int
	start       string
	active      *net.UnixConn
	accepted    int
	failure     error
}

func newGuardTransport(ctx context.Context, p *Provider, v Preview) (*guardTransport, error) {
	dir, err := os.MkdirTemp("/tmp", "cdots-")
	if err != nil {
		return nil, fmt.Errorf("create private guard directory: %w", err)
	}
	path := filepath.Join(dir, "guard")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, errors.Join(err, os.Remove(dir))
	}
	if err = os.Chmod(path, 0600); err != nil {
		return nil, errors.Join(err, listener.Close(), os.RemoveAll(dir))
	}
	t := &guardTransport{dir: dir, path: path, listener: listener, ctx: ctx, provider: p, preview: v, ready: make(chan struct{}), done: make(chan struct{})}
	go t.serve()
	return t, nil
}

func (t *guardTransport) bind(pid int) error {
	start, _, err := processIdentity(pid)
	if err != nil {
		return errors.New("live apt process identity unavailable")
	}
	t.mu.Lock()
	t.pid, t.start = pid, start
	t.mu.Unlock()
	close(t.ready)
	return nil
}

func (t *guardTransport) serve() {
	defer close(t.done)
	for {
		conn, err := t.listener.AcceptUnix()
		if err != nil {
			return
		}
		t.mu.Lock()
		t.active = conn
		t.mu.Unlock()
		err = t.validate(conn)
		if err == nil {
			t.mu.Lock()
			t.accepted++
			t.mu.Unlock()
			_, err = conn.Write([]byte{1})
		} else {
			_, writeErr := conn.Write([]byte{0})
			err = errors.Join(err, writeErr)
		}
		closeErr := conn.Close()
		if errors.Is(closeErr, net.ErrClosed) {
			closeErr = nil
		}
		t.mu.Lock()
		t.active = nil
		if err != nil || closeErr != nil {
			t.failure = errors.Join(t.failure, err, closeErr)
			if t.cancel != nil {
				t.cancel()
			}
		}
		t.mu.Unlock()
		if err != nil || closeErr != nil {
			listenerErr := t.listener.Close()
			if listenerErr != nil && !errors.Is(listenerErr, net.ErrClosed) {
				t.mu.Lock()
				t.failure = errors.Join(t.failure, listenerErr)
				t.mu.Unlock()
			}
			return
		}
	}
}

func (t *guardTransport) validate(conn *net.UnixConn) error {
	if err := conn.SetDeadline(time.Now().Add(simulationTimeout)); err != nil {
		return err
	}
	select {
	case <-t.ctx.Done():
		return t.ctx.Err()
	case <-t.ready:
	}
	t.mu.Lock()
	pid, start, count, failure := t.pid, t.start, t.accepted, t.failure
	hooks := append([]string(nil), t.hooks...)
	t.mu.Unlock()
	if failure != nil {
		return errors.New("guard invocation already rejected")
	}
	if count != 0 {
		return errors.New("replayed or multiple native guard calls; fresh full authorization required")
	}
	peer, err := guardPeer(conn)
	if err != nil || peer.Uid != uint32(os.Geteuid()) || peer.Pid <= 0 {
		return errors.New("foreign guard peer")
	}
	if err = checkGuardOrigin(int(peer.Pid), pid, start); err != nil {
		return err
	}
	var header [4]byte
	if _, err = io.ReadFull(conn, header[:]); err != nil {
		return errors.New("incomplete guard frame")
	}
	n := binary.BigEndian.Uint32(header[:])
	if n == 0 || n > maxCommandOutput {
		return errors.New("guard frame limit exceeded")
	}
	data := make([]byte, int(n))
	if _, err = io.ReadFull(conn, data); err != nil {
		return errors.New("incomplete guard payload")
	}
	// The helper half-closes after exactly one frame. Extra bytes/replay fail.
	var extra [1]byte
	if n, err := conn.Read(extra[:]); n != 0 || !errors.Is(err, io.EOF) {
		return errors.New("trailing guard payload")
	}
	if err = checkEffectiveHookConfig(data, hooks); err != nil {
		return err
	}
	// Do not consume native evidence through a newly redirected query profile.
	if t.checkHost == nil {
		return errors.New("missing native host profile validation")
	}
	if err = t.checkHost(); err != nil {
		return err
	}
	if err = t.provider.ValidateHook(t.ctx, t.preview, bytes.NewReader(data)); err != nil {
		return err
	}
	// Refresh filesystem trust AND native dpkg options at the authenticated,
	// single-use boundary, after the read-only metadata queries. APT's dump
	// cannot expose dpkg.cfg. Known dpkg-preconfigure remains after this guard;
	// trusted root/package-script side effects are not atomic or continuously
	// constrained by this snapshot. No root adversary/rollback claim is made.
	if err = t.checkHost(); err != nil {
		return err
	}
	// Check the exact apt process remains live after all profile queries.
	return checkGuardOrigin(int(peer.Pid), pid, start)
}

func (t *guardTransport) result() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.accepted != 1 {
		return errors.Join(errors.New("apt exited without one exact verified native guard; resulting state uncertain"), t.failure)
	}
	return t.failure
}

func (t *guardTransport) close() error {
	t.closeOnce.Do(func() { t.closeErr = t.closeOwned() })
	return t.closeErr
}

func (t *guardTransport) closeOwned() error {
	listenerErr := t.listener.Close()
	if errors.Is(listenerErr, net.ErrClosed) {
		listenerErr = nil
	}
	t.mu.Lock()
	if t.active != nil {
		if err := t.active.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.failure = errors.Join(t.failure, err)
		}
	}
	t.mu.Unlock()
	// bind may not have run if process Start failed; release the server's wait.
	select {
	case <-t.ready:
	default:
		close(t.ready)
	}
	<-t.done
	// The directory was exclusively created by this invocation. It contains only
	// our socket and command configuration, never user data or approval records.
	return errors.Join(listenerErr, os.RemoveAll(t.dir))
}

func guardPeer(conn *net.UnixConn) (*syscall.Ucred, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var cred *syscall.Ucred
	var sockErr error
	err = raw.Control(func(fd uintptr) {
		cred, sockErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})
	return cred, errors.Join(err, sockErr)
}

// Linux stat field 22 is the process start time. Parse after the final ')' so
// arbitrary process comm text cannot impersonate parent/start-time columns.
func processIdentity(pid int) (start string, parent int, err error) {
	if pid <= 0 {
		return "", 0, errors.New("invalid process identity")
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil || len(data) > 4096 {
		return "", 0, errors.New("process identity unavailable")
	}
	i := strings.LastIndexByte(string(data), ')')
	if i < 0 {
		return "", 0, errors.New("malformed process identity")
	}
	fields := strings.Fields(string(data[i+1:]))
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return "", 0, errors.New("expired process identity")
	}
	parent, err = strconv.Atoi(fields[1])
	if err != nil {
		return "", 0, err
	}
	return fields[19], parent, nil
}

func checkGuardOrigin(peer, aptPID int, start string) error {
	actual, _, err := processIdentity(aptPID)
	if err != nil || actual != start || aptPID <= 0 {
		return errors.New("expired apt invocation")
	}
	peerExe, err := os.Stat("/proc/" + strconv.Itoa(peer) + "/exe")
	if err != nil {
		return errors.New("guard executable unavailable")
	}
	ownExe, err := os.Stat("/proc/self/exe")
	if err != nil || !os.SameFile(ownExe, peerExe) {
		return errors.New("foreign guard executable")
	}
	for depth := 0; depth < 8; depth++ {
		if peer == aptPID {
			return nil
		}
		_, parent, err := processIdentity(peer)
		if err != nil || parent <= 1 || parent == peer {
			break
		}
		peer = parent
	}
	return errors.New("guard peer is not a descendant of the exact live apt process")
}

func checkEffectiveHookConfig(data []byte, hooks []string) error {
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || lines[0] != "VERSION 3" {
		return errors.New("unsupported native guard protocol")
	}
	cfg := executionSettings{}
	ended := false
	for _, line := range lines[1:] {
		if line == "" {
			ended = true
			break
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return errors.New("malformed effective guard configuration")
		}
		key, err := decodeHookConfig(key, true)
		if err != nil {
			return err
		}
		value, err = decodeHookConfig(value, false)
		if err != nil {
			return err
		}
		cfg[strings.ToLower(key)] = append(cfg[strings.ToLower(key)], value)
	}
	if !ended {
		return errors.New("incomplete effective guard configuration")
	}
	actual, err := checkExecutionConfig(cfg, true)
	if err != nil {
		return err
	}
	if len(actual) != len(hooks) {
		return errors.New("effective native guard hook list drift")
	}
	for i := range hooks {
		if actual[i] != hooks[i] {
			return errors.New("native guard must run first with exact invocation arguments")
		}
	}
	token, _, _ := strings.Cut(hooks[0], " ")
	required := map[string]string{
		"dpkg::use-pty": "false",
		"binary":        "apt-get",
		"dpkg::tools::options::" + token + "::version": "3",
		"dpkg::tools::options::" + token + "::infofd":  "0",
		"quiet": "0", "apt::get::quiet": "0",
	}
	for _, key := range executionFalseKeys {
		required[strings.ToLower(key)] = "false"
	}
	for key, want := range required {
		values := cfg[key]
		if len(values) != 1 || values[0] != want {
			return errors.New("missing or changed effective native guard safety setting")
		}
	}
	// No other options for the guard executable may change native dispatch.
	prefix := "dpkg::tools::options::" + token + "::"
	for key := range cfg {
		if strings.HasPrefix(key, prefix) && key != prefix+"version" && key != prefix+"infofd" {
			return errors.New("unexpected native guard option")
		}
	}
	return nil
}

// RunInternalGuard is solely the native fixed hook helper; it cannot authorize a
// transaction independently. Its parent must own the private invocation socket.
// No alternate executable/socket path or test privilege switch is exposed.
func RunInternalGuard(args []string, stdin io.Reader) error {
	if os.Geteuid() != 0 {
		return errors.New("internal guard requires root")
	}
	if len(args) != 1 || !validGuardPath(args[0]) {
		return errors.New("invalid internal guard invocation")
	}
	if os.Getenv("APT_HOOK_INFO_FD") != "0" || os.Getenv("DPKG_FRONTEND_LOCKED") != "true" {
		return errors.New("native hook fd/frontend lock evidence missing")
	}
	info, err := os.Lstat(filepath.Dir(args[0]))
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return errors.New("private guard directory unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return errors.New("private guard directory not root-owned")
	}
	return guardClient(args[0], stdin)
}

func validGuardPath(path string) bool {
	if !strings.HasPrefix(path, "/tmp/cdots-") || !strings.HasSuffix(path, "/guard") {
		return false
	}
	middle := strings.TrimSuffix(strings.TrimPrefix(path, "/tmp/cdots-"), "/guard")
	if middle == "" || len(middle) > 32 {
		return false
	}
	for _, c := range middle {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func guardClient(path string, stdin io.Reader) (ret error) {
	ctx, cancel := context.WithTimeout(context.Background(), simulationTimeout)
	defer cancel()
	// APT inherits a blocking pipe on fd 0. Register a nonblocking duplicate
	// with Go's poller so protocol reads can actually honor deadlines.
	if file, ok := stdin.(*os.File); ok {
		fd, err := syscall.Dup(int(file.Fd()))
		if err != nil {
			return errors.New("native hook input unavailable")
		}
		if err = syscall.SetNonblock(fd, true); err != nil {
			return errors.Join(err, syscall.Close(fd))
		}
		input := os.NewFile(uintptr(fd), "native-hook-input")
		defer func() { ret = errors.Join(ret, input.Close()) }()
		stdin = input
	}
	data, err := readHook(ctx, stdin)
	if err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", path, simulationTimeout)
	if err != nil {
		return errors.New("invocation guard unavailable")
	}
	defer func() { ret = errors.Join(ret, conn.Close()) }()
	if err = conn.SetDeadline(time.Now().Add(simulationTimeout)); err != nil {
		return err
	}
	unix, ok := conn.(*net.UnixConn)
	if !ok {
		return errors.New("invalid guard transport")
	}
	cred, err := guardPeer(unix)
	if err != nil || cred.Uid != uint32(os.Geteuid()) {
		return errors.New("foreign guard coordinator")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	if _, err = io.Copy(conn, io.MultiReader(bytes.NewReader(header[:]), bytes.NewReader(data))); err != nil {
		return errors.New("guard request failed")
	}
	if err = unix.CloseWrite(); err != nil {
		return err
	}
	var response [1]byte
	if _, err = io.ReadFull(conn, response[:]); err != nil || response[0] != 1 {
		return errors.New("native transaction guard rejected execution")
	}
	return nil
}
