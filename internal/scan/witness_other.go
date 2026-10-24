//go:build !linux

package scan

import "os"

// WitnessOf returns the timestamps that prove a fact about info (README D27).
func WitnessOf(info os.FileInfo) Witness {
	return Witness{Size: info.Size(), Mod: info.ModTime(), Change: info.ModTime()}
}
