//go:build linux

package itest

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

// RunOnTTY runs duls with stdout on a pseudo-terminal of cols columns. It
// proves the character-device branch of the colour and width gates end to
// end.
func RunOnTTY(t *testing.T, dir string, cols int, args ...string) Result {
	t.Helper()
	return RunOnTTYEnv(t, dir, cols, nil, args...)
}

// RunVisual runs duls on a pseudo-terminal and returns the answer as its
// layout text: the frame erased, the strip and its blank line dropped, every
// escape byte gone, and every row box turned back into its margin (Layout).
func RunVisual(t *testing.T, dir string, cols int, args ...string) Result {
	t.Helper()
	return RunVisualEnv(t, dir, cols, nil, args...)
}

// RunVisualEnv runs duls on a pseudo-terminal with env over the sanitized
// environment and returns the answer as its layout text.
func RunVisualEnv(t *testing.T, dir string, cols int, env []string, args ...string) Result {
	t.Helper()
	res := RunOnTTYEnv(t, dir, cols, env, args...)
	res.Stdout = Layout(res.Stdout)
	return res
}

// RunOnTTYEnv runs duls on a pseudo-terminal with env over the sanitized
// environment.
func RunOnTTYEnv(t *testing.T, dir string, cols int, env []string, args ...string) Result {
	t.Helper()
	master, slave := openPTY(t)
	defer master.Close()
	if cols > 0 {
		if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(cols), Row: 24}); err != nil {
			t.Fatalf("set the terminal size: %v", err)
		}
	}
	// Disable output post-processing, so the terminal never rewrites LF to
	// CRLF and the captured bytes are exact.
	termios, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatalf("read the terminal settings: %v", err)
	}
	termios.Oflag &^= unix.OPOST
	if err := unix.IoctlSetTermios(int(slave.Fd()), unix.TCSETS, termios); err != nil {
		t.Fatalf("write the terminal settings: %v", err)
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	cmd.Env = Env(env...)
	cmd.Stdout = slave
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	if err := cmd.Start(); err != nil {
		slave.Close()
		t.Fatalf("start duls: %v", err)
	}
	slave.Close()

	type readResult struct {
		data []byte
		err  error
	}
	drained := make(chan readResult, 1)
	go func() {
		var b bytes.Buffer
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				b.Write(buf[:n])
			}
			if err != nil {
				if errors.Is(err, unix.EIO) || errors.Is(err, io.EOF) {
					drained <- readResult{b.Bytes(), nil}
					return
				}
				drained <- readResult{b.Bytes(), err}
				return
			}
		}
	}()
	err = cmd.Wait()
	res := <-drained
	if res.err != nil {
		t.Fatalf("read the terminal: %v", res.err)
	}
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("wait for duls: %v", err)
	}
	return Result{Code: code, Stdout: string(res.data), Stderr: errOut.String()}
}

func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("a pseudo-terminal is required: %v", err)
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		t.Skipf("a pseudo-terminal is required: %v", err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		master.Close()
		t.Skipf("a pseudo-terminal is required: %v", err)
	}
	slave, err = os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR, 0)
	if err != nil {
		master.Close()
		t.Skipf("a pseudo-terminal is required: %v", err)
	}
	return master, slave
}
