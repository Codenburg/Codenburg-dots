//go:build linux

package apt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

var terminalExecution = make(chan struct{}, 1)

// runAPTProcess owns a fresh process group, transfers a real terminal to that
// group, and always restores foreground ownership. Cancellation kills the entire
// group, not just apt (which could leave dpkg running). Execute requires the
// verified APT 2.6.1 Use-Pty=false profile: dpkgpm.cc's SetupSlavePtyMagic
// returns before setsid, and fileutl.cc's ExecFork preserves this group/TTY.
// Intentionally daemonized maintainer-script services are not covered. No shell
// is launched here. A foreground-only Ctrl-C may not reach the coordinator;
// native nonzero/signalled exit is still an error, followed by full group cleanup.
func runAPTProcess(ctx context.Context, cmd *exec.Cmd, bind func(int) error) (ret error) {
	select {
	case terminalExecution <- struct{}{}:
		defer func() { <-terminalExecution }()
	case <-ctx.Done():
		return ctx.Err()
	}
	var terminal *os.File
	var foreground int32
	if file, ok := cmd.Stdin.(*os.File); ok {
		err := terminalIOCTL(file.Fd(), syscall.TIOCGPGRP, &foreground)
		if err == nil {
			if int(foreground) != syscall.Getpgrp() {
				return errors.New("interactive input is not owned by the foreground coordinator")
			}
			terminal = file
		} else if !errors.Is(err, syscall.ENOTTY) {
			return fmt.Errorf("inspect interactive terminal: %w", err)
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	// Even successful APT exit must not leave a native hook/child behind in our
	// process group. ESRCH simply means the group is already gone.
	defer func() {
		err := syscall.Kill(-pid, syscall.SIGKILL)
		if err != nil && !errors.Is(err, syscall.ESRCH) {
			ret = errors.Join(ret, err)
		}
	}()
	if terminal != nil {
		child := int32(pid)
		if err := terminalIOCTL(terminal.Fd(), syscall.TIOCSPGRP, &child); err != nil {
			return errors.Join(err, cmd.Cancel(), cmd.Wait())
		}
		defer func() { ret = errors.Join(ret, terminalIOCTL(terminal.Fd(), syscall.TIOCSPGRP, &foreground)) }()
		// A child that read during Start/foreground transfer can have received
		// SIGTTIN; resume it only after it really owns the terminal.
		if err := syscall.Kill(-pid, syscall.SIGCONT); err != nil && !errors.Is(err, syscall.ESRCH) {
			return errors.Join(err, cmd.Cancel(), cmd.Wait())
		}
	}
	if err := bind(pid); err != nil {
		return errors.Join(err, cmd.Cancel(), cmd.Wait())
	}
	return errors.Join(cmd.Wait(), ctx.Err())
}

// Restoring the terminal is itself a background-group ioctl. Block SIGTTOU on
// this OS thread for this syscall only; do not alter global signal dispositions
// or leave the caller's signal mask changed. Linux rt_sigprocmask uses 8 bytes.
func terminalIOCTL(fd, request uintptr, value *int32) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	mask := uint64(1) << uint(syscall.SIGTTOU-1)
	var old uint64
	_, _, errno := syscall.RawSyscall6(syscall.SYS_RT_SIGPROCMASK, 0, uintptr(unsafe.Pointer(&mask)), uintptr(unsafe.Pointer(&old)), 8, 0, 0)
	if errno != 0 {
		return errno
	}
	_, _, ioctlErr := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(unsafe.Pointer(value)))
	_, _, restoreErr := syscall.RawSyscall6(syscall.SYS_RT_SIGPROCMASK, 2, uintptr(unsafe.Pointer(&old)), 0, 8, 0, 0)
	var errs []error
	if ioctlErr != 0 {
		errs = append(errs, ioctlErr)
	}
	if restoreErr != 0 {
		errs = append(errs, restoreErr)
	}
	return errors.Join(errs...)
}
