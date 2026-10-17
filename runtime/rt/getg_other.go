//go:build !amd64 && !arm64

package rt

import (
	"runtime"
	"strconv"
	"strings"
)

// getg identifies the current goroutine by its id, parsed from the header
// of its stack trace ("goroutine 18 [running]:"). It is much slower than
// the assembly versions for amd64 and arm64, but ids are never reused.
func getg() uintptr {
	var buf [64]byte
	s := string(buf[:runtime.Stack(buf[:], false)])
	s = strings.TrimPrefix(s, "goroutine ")
	if i := strings.IndexByte(s, ' '); i >= 0 {
		s = s[:i]
	}
	id, _ := strconv.ParseUint(s, 10, 64)
	return uintptr(id)
}
