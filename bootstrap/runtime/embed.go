// Package runtime embeds the Go sources of the runtime library that the
// bootstrap compiler links into the programs it builds. It is a frozen copy
// of ../../runtime from before evidence passing: the bootstrap compiler
// generates code for this calling convention only, and the programs it
// builds (the first stage of the self-hosted compiler) run on one goroutine
// at a time.
package runtime

import "embed"

//go:embed rt/*.go
var FS embed.FS
