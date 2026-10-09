//go:build linux

package cli

import (
	"context"
	"errors"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func privatePTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	fd, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(fd), "test-pty-master")
	t.Cleanup(func() {
		if err := master.Close(); err != nil {
			t.Error(err)
		}
	})
	var unlock int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), 0x40045431, uintptr(unsafe.Pointer(&unlock)))
	if errno != 0 {
		t.Fatal(errno)
	}
	var number int32
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), 0x80045430, uintptr(unsafe.Pointer(&number)))
	if errno != 0 {
		t.Fatal(errno)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(int(number)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := slave.Close(); err != nil {
			t.Error(err)
		}
	})
	return master, slave
}
func TestTerminalCapabilityAndNullRejection(t *testing.T) {
	master, slave := privatePTY(t)
	if !terminalCapable(master) || !terminalCapable(slave) {
		t.Fatal("PTY not recognized")
	}
	f, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		t.Fatal("fixture is not char device")
	}
	if terminalCapable(f) {
		t.Fatal("/dev/null mistaken for terminal")
	}
	b, e := applyFixture()
	err = RunInteractive(context.Background(), []string{"apply", "git"}, f, f, f, fakeSystem{}, b, e)
	if err == nil || e.calls != 0 {
		t.Fatalf("null approval: %v calls=%d", err, e.calls)
	}
}
func TestTerminalReadCancellationAndNativeInputPreserved(t *testing.T) {
	master, slave := privatePTY(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := terminalLine(ctx, slave)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("not cancellable: %v", err)
	}
	if _, err = master.Write([]byte("apply\ny\n")); err != nil {
		t.Fatal(err)
	}
	b, e := applyFixture()
	e.run = func(ctx context.Context) error {
		native, err := terminalLine(ctx, slave)
		if err != nil || native != "y" {
			t.Fatalf("native answer prefetched: %q %v", native, err)
		}
		b.installed = true
		return nil
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err = RunInteractive(ctx2, []string{"apply", "git"}, slave, slave, slave, fakeSystem{}, b, e); err != nil || e.calls != 1 {
		t.Fatalf("%v calls=%d", err, e.calls)
	}
}
