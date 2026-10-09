//go:build linux

package apt

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestAPTProcessForegroundTerminal(t *testing.T) {
	exerciseAPTTerminal(t, false)
}

func TestAPTProcessTerminalInterrupt(t *testing.T) {
	exerciseAPTTerminal(t, true)
}

func exerciseAPTTerminal(t *testing.T, interrupt bool) {
	t.Helper()
	fd, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(fd), "test-pty-master")
	defer master.Close()
	var unlock int32
	if err := terminalIOCTL(master.Fd(), 0x40045431, &unlock); err != nil {
		t.Fatal(err)
	} // TIOCSPTLCK
	var number int32
	if err := terminalIOCTL(master.Fd(), 0x80045430, &number); err != nil {
		t.Fatal(err)
	} // TIOCGPTN
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(int(number)), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer slave.Close()
	// File.Fd may temporarily switch a pollable file back to blocking mode.
	if err := syscall.SetNonblock(int(master.Fd()), true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mode := "tty-coordinator"
	if interrupt {
		mode = "tty-coordinator-interrupt"
	}
	marker := filepath.Join(t.TempDir(), "tty-dpkg-pid")
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAPTProcessHelper$", "--", mode, marker)
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Error(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Log(err)
			}
		}
	}()
	if err := master.SetReadDeadline(time.Now().Add(4 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	buf := make([]byte, 512)
	for !strings.Contains(output.String(), "native prompt:") {
		n, err := master.Read(buf)
		output.Write(buf[:n])
		if err != nil {
			t.Fatalf("prompt stopped by background tty: %v %s", err, output.String())
		}
	}
	response := "chosen response\n"
	if interrupt {
		response = "\x03"
	} // Real terminal ISIG delivery, not a canned yes.
	if _, err := master.Write([]byte(response)); err != nil {
		t.Fatal(err)
	}
	for !strings.Contains(output.String(), "foreground restored") {
		n, err := master.Read(buf)
		output.Write(buf[:n])
		if err != nil {
			t.Fatalf("terminal completion: %v %s", err, output.String())
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("tty helper: %v %s", err, output.String())
	}
	if interrupt {
		if !strings.Contains(output.String(), "native stopped; state may be partial") || !strings.Contains(output.String(), "coordinator did not receive SIGINT") {
			t.Fatal(output.String())
		}
		data, err := os.ReadFile(marker)
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(string(data))
		if err != nil {
			t.Fatal(err)
		}
		assertExpiredProcess(t, pid)
	} else if !strings.Contains(output.String(), "received chosen response") {
		t.Fatal(output.String())
	}
}

func TestAPTProcessCancellationKillsDescendants(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "descendant-pid")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAPTProcessHelper$", "--", "descendant-parent", marker)
	cmd.Stdin = strings.NewReader("")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	err := runAPTProcess(ctx, cmd, func(int) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout not propagated: %v", err)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal("descendant not exercised", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	assertExpiredProcess(t, pid)
}

func assertExpiredProcess(t *testing.T, pid int) {
	t.Helper()
	// A reparented zombie may await the container init's reap; it is not a live
	// orphan executing dpkg. processIdentity rejects zombies as expired.
	end := time.Now().Add(2 * time.Second)
	for {
		_, _, err := processIdentity(pid)
		if err != nil {
			break
		}
		if time.Now().After(end) {
			t.Fatal("live descendant survived group cancellation")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAPTProcessQueuedCancellation(t *testing.T) {
	terminalExecution <- struct{}{}
	defer func() { <-terminalExecution }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "never-started")
	err := runAPTProcess(ctx, cmd, func(int) error { t.Fatal("cancelled process bound"); return nil })
	if !errors.Is(err, context.Canceled) || cmd.Process != nil {
		t.Fatalf("queued cancellation lost: %v", err)
	}
}

func TestAPTProcessHelper(t *testing.T) {
	i := 0
	for i < len(os.Args) && os.Args[i] != "--" {
		i++
	}
	if i == len(os.Args) {
		return
	}
	args := os.Args[i+1:]
	if len(args) == 0 {
		os.Exit(80)
	}
	switch args[0] {
	case "tty-coordinator", "tty-coordinator-interrupt":
		interrupt := args[0] == "tty-coordinator-interrupt"
		interrupts := make(chan os.Signal, 1)
		signal.Notify(interrupts, syscall.SIGINT)
		defer signal.Stop(interrupts)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		mode := "tty-front-no-pty"
		if interrupt {
			mode = "tty-front-no-pty-interrupt"
		}
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAPTProcessHelper$", "--", mode, args[1])
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := runAPTProcess(ctx, cmd, func(int) error { return nil })
		if interrupt {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.Success() || ctx.Err() != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(81)
			}
			fmt.Fprintln(os.Stdout, "native stopped; state may be partial")
			select {
			case <-interrupts:
				os.Exit(88)
			default:
				fmt.Fprintln(os.Stdout, "coordinator did not receive SIGINT")
			}
		} else if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(81)
		}
		var foreground int32
		if err := terminalIOCTL(os.Stdin.Fd(), syscall.TIOCGPGRP, &foreground); err != nil || int(foreground) != syscall.Getpgrp() {
			os.Exit(82)
		}
		fmt.Fprintln(os.Stdout, "foreground restored")
	case "tty-front-no-pty", "tty-front-no-pty-interrupt":
		// Model dpkgpm.cc:1210/1309/2008 and fileutl.cc:898: no native
		// PTY => fork/exec without setsid/setpgid, with inherited operator TTY.
		mode := "tty-reader"
		if args[0] == "tty-front-no-pty-interrupt" {
			mode = "tty-reader-ignore-int"
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestAPTProcessHelper$", "--", mode, strconv.Itoa(syscall.Getpgrp()), args[1])
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			os.Exit(89)
		}
	case "tty-reader", "tty-reader-ignore-int":
		expectedGroup, err := strconv.Atoi(args[1])
		if err != nil || syscall.Getpgrp() != expectedGroup {
			os.Exit(90)
		}
		if args[0] == "tty-reader-ignore-int" {
			// Even an interrupt-ignoring script wait must not survive frontend exit.
			signal.Ignore(syscall.SIGINT)
			if err := os.WriteFile(args[2], []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
				os.Exit(91)
			}
		}
		var foreground int32
		if err := terminalIOCTL(os.Stdin.Fd(), syscall.TIOCGPGRP, &foreground); err != nil || int(foreground) != syscall.Getpgrp() {
			os.Exit(83)
		}
		fmt.Fprint(os.Stdout, "native prompt:")
		answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			os.Exit(84)
		}
		fmt.Fprintln(os.Stdout, "received "+strings.TrimSpace(answer))
	case "descendant-parent":
		cmd := exec.Command(os.Args[0], "-test.run=^TestAPTProcessHelper$", "--", "descendant", args[1])
		if err := cmd.Run(); err != nil {
			os.Exit(85)
		}
	case "descendant":
		if err := os.WriteFile(args[1], []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			os.Exit(86)
		}
		time.Sleep(20 * time.Second)
	default:
		os.Exit(87)
	}
	os.Exit(0)
}
