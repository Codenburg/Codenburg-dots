//go:build linux

package cli

import (
	"context"
	"errors"
	"os"
	"syscall"
	"unsafe"
)

func terminalCapable(stream any) bool {
	f, ok := stream.(*os.File)
	if !ok || f == nil {
		return false
	}
	var term syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&term)))
	return errno == 0
}

func terminalLine(ctx context.Context, f *os.File) (string, error) {
	if !terminalCapable(f) {
		return "", errors.New("genuine terminal stdin required")
	}
	return readScalarLine(ctx, terminalReader{ctx: ctx, file: f})
}

type terminalReader struct {
	ctx  context.Context
	file *os.File
}

func (r terminalReader) Read(p []byte) (int, error) {
	fd := int(r.file.Fd())
	var set syscall.FdSet
	if fd < 0 || fd >= len(set.Bits)*64 {
		return 0, errors.New("terminal descriptor outside readiness polling range")
	}
	for {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		set = syscall.FdSet{}
		set.Bits[fd/64] |= 1 << uint(fd%64)
		timeout := syscall.Timeval{Usec: 50000}
		n, err := syscall.Select(fd+1, &set, nil, nil, &timeout)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return 0, err
		}
		if n == 0 {
			continue
		}
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		// The application owns stdin exclusively at this point. Read one byte only;
		// APT later receives the same original *os.File, with remaining input intact.
		n, err = syscall.Read(fd, p[:1])
		if err == syscall.EINTR {
			continue
		}
		return n, err
	}
}
