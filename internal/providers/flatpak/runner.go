package flatpak

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	ErrOutputLimit = errors.New("flatpak inspection output limit exceeded")
	ErrExecution   = errors.New("isolated flatpak inspection failed")
)

// ExecRunner executes only allowlisted inspections in mandatory Bubblewrap isolation.
// Its zero value fails closed. NewExecRunner detects tools without executing them.
type ExecRunner struct {
	flatpak, bwrap, home, data string
	timeout                    time.Duration
	maxOutput                  int
	command                    func(context.Context, string, ...string) *exec.Cmd
}

func NewExecRunner() (*ExecRunner, error) {
	return detect(exec.LookPath, runtime.GOOS, os.Getenv("HOME"), os.Getenv("XDG_DATA_HOME"))
}
func detect(lookup func(string) (string, error), goos, home, data string) (*ExecRunner, error) {
	if goos != "linux" {
		return nil, fmt.Errorf("flatpak isolation is unsupported on %s", goos)
	}
	if !readablePath(home) {
		return nil, errors.New("flatpak inspection requires an absolute HOME outside sandbox scratch mounts")
	}
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	if !readablePath(data) {
		return nil, errors.New("flatpak inspection requires an absolute XDG_DATA_HOME outside sandbox scratch mounts")
	}
	paths := map[string]string{}
	for _, name := range []string{"flatpak", "bwrap"} {
		path, err := lookup(name)
		if err != nil {
			return nil, fmt.Errorf("required inspection executable %s: %w", name, err)
		}
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("required executable %s resolved to a relative path", name)
		}
		paths[name] = path
	}
	return &ExecRunner{flatpak: paths["flatpak"], bwrap: paths["bwrap"], home: home, data: data,
		timeout: 10 * time.Second, maxOutput: 4 * 1024 * 1024, command: exec.CommandContext}, nil
}
func readablePath(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n") {
		return false
	}
	for _, mount := range []string{"/run", "/tmp", "/dev", "/proc"} {
		if path == mount || strings.HasPrefix(path, mount+"/") {
			return false
		}
	}
	return true
}
func controlledEnv(home, data string) []string {
	return []string{"HOME=" + home, "XDG_DATA_HOME=" + data, "PATH=/usr/bin:/bin", "LC_ALL=C", "TERM=dumb",
		"FLATPAK_FANCY_OUTPUT=0", "DBUS_SESSION_BUS_ADDRESS=disabled:", "DBUS_SYSTEM_BUS_ADDRESS=disabled:"}
}
func scopeFlag(scope Scope) string {
	if strings.HasPrefix(string(scope), "installation:") {
		return "--installation=" + strings.TrimPrefix(string(scope), "installation:")
	}
	return "--" + string(scope)
}
func (q Query) Validate() error {
	if err := q.Scope.Validate(); err != nil {
		return err
	}
	switch q.Operation {
	case List, Remotes:
		if q.Remote != "" {
			return errors.New("remote operand is only permitted for remote-ls")
		}
	case RemoteLS:
		if !validOperand(q.Remote) {
			return fmt.Errorf("invalid remote operand %q", q.Remote)
		}
	default:
		return fmt.Errorf("unsupported flatpak inspection operation %q", q.Operation)
	}
	return nil
}
func (r *ExecRunner) invocation(q Query) ([]string, []string, error) {
	if err := q.Validate(); err != nil {
		return nil, nil, err
	}
	if r == nil || r.command == nil || !filepath.IsAbs(r.flatpak) || !filepath.IsAbs(r.bwrap) {
		return nil, nil, errors.New("flatpak isolated runner is not initialized")
	}
	args := []string{"--ro-bind", "/", "/", "--unshare-all", "--unshare-user", "--unshare-net", "--unshare-pid",
		"--cap-drop", "ALL", "--disable-userns", "--new-session", "--die-with-parent", "--proc", "/proc",
		"--dev", "/dev", "--tmpfs", "/run", "--tmpfs", "/tmp", "--chdir", "/", "--clearenv"}
	env := controlledEnv(r.home, r.data)
	for _, value := range env {
		name, value, _ := strings.Cut(value, "=")
		args = append(args, "--setenv", name, value)
	}
	args = append(args, "--", r.flatpak, string(q.Operation), scopeFlag(q.Scope))
	switch q.Operation {
	case List:
		args = append(args, "--app", "--columns=ref:full,origin:full,version:full")
	case Remotes:
		args = append(args, "--show-disabled", "--columns=name:full,options:full")
	case RemoteLS:
		args = append(args, "--cached", "--app", "--all", "--arch=*", "--columns=ref:full,version:full", q.Remote)
	}
	return args, env, nil
}

func (r *ExecRunner) Run(ctx context.Context, q Query) (string, error) {
	args, env, err := r.invocation(q)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("flatpak inspection context: %w", err)
	}
	deadlineCtx, deadlineCancel := context.WithTimeout(ctx, r.timeout)
	defer deadlineCancel()
	processCtx, processCancel := context.WithCancel(deadlineCtx)
	defer processCancel()
	cmd := r.command(processCtx, r.bwrap, args...)
	cmd.Env = env
	cmd.Dir = "/"
	cmd.Stdin = nil
	cmd.ExtraFiles = nil
	cmd.WaitDelay = time.Second
	stdout := &boundedOutput{limit: r.maxOutput, cancel: processCancel}
	stderr := &boundedOutput{limit: r.maxOutput, cancel: processCancel}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	if stdout.exceeded || stderr.exceeded {
		return "", ErrOutputLimit
	}
	if deadlineCtx.Err() != nil {
		return "", fmt.Errorf("flatpak inspection deadline or cancellation: %w", deadlineCtx.Err())
	}
	if err != nil {
		return "", fmt.Errorf("%w (%s; sandbox support is mandatory; stderr %q): %w",
			ErrExecution, q.Operation, stderr.buffer.String(), err)
	}
	if stderr.buffer.Len() > 0 {
		return "", fmt.Errorf("%w: inspection emitted stderr: %q", ErrExecution, stderr.buffer.String())
	}
	return stdout.buffer.String(), nil
}

// os/exec copies each stream to its own writer and joins those copies before Run returns.
// Output overruns cancel execution immediately; retained bytes are bounded per stream.
type boundedOutput struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()
	if len(p) > remaining {
		b.exceeded = true
		b.cancel()
		if remaining > 0 {
			if _, err := b.buffer.Write(p[:remaining]); err != nil {
				return 0, err
			}
		}
		return 0, ErrOutputLimit
	}
	return b.buffer.Write(p)
}
