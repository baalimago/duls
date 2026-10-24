//go:build linux

package tui

import (
	"os"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

func TestFormOnATerminal(t *testing.T) {
	master, slave := openPTY(t)
	defer master.Close()
	defer slave.Close()
	if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: 72, Row: 24}); err != nil {
		t.Fatalf("set the terminal size: %v", err)
	}
	if !IsTerminal(slave) {
		t.Fatal("the pseudo-terminal is not a terminal")
	}
	if got := Width(slave); got != 72 {
		t.Fatalf("Width = %d, want 72", got)
	}
	trueColor := func(name string) string {
		if name == "COLORTERM" {
			return "truecolor"
		}
		return ""
	}
	if got := DetectForm(slave, trueColor); got != Visual {
		t.Fatalf("DetectForm = %v, want Visual on a terminal", got)
	}
	if got := DepthOf(trueColor).depth; got != TrueColor {
		t.Fatalf("DepthOf = %v, want TrueColor", got)
	}
	if got := DepthOf(func(string) string { return "" }).depth; got != Basic {
		t.Fatalf("DepthOf without COLORTERM = %v, want Basic", got)
	}
	noColor := DetectForm(slave, func(name string) string {
		if name == "NO_COLOR" {
			return "1"
		}
		return ""
	})
	if noColor != Agent {
		t.Fatalf("DetectForm with NO_COLOR = %v, want Agent", noColor)
	}
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
