//go:build linux

package scan

import (
	"os"
	"syscall"
	"time"
)

// WitnessOf returns the timestamps that prove a fact about info (README D27).
func WitnessOf(info os.FileInfo) Witness {
	w := Witness{Size: info.Size(), Mod: info.ModTime(), Change: info.ModTime()}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		w.Dev = uint64(st.Dev)
		w.Ino = st.Ino
		w.Mod = time.Unix(int64(st.Mtim.Sec), int64(st.Mtim.Nsec))
		w.Change = time.Unix(int64(st.Ctim.Sec), int64(st.Ctim.Nsec))
	}
	return w
}
