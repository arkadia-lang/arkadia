//go:build amd64 || arm64

package rt

// getg returns the address of the runtime's descriptor of the current
// goroutine (getg_*.s). It identifies the goroutine while it runs.
func getg() uintptr
