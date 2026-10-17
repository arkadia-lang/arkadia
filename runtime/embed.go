// Package runtime embeds the Go sources of the Arkadia runtime library so
// that the compiler can copy them next to generated programs.
package runtime

import "embed"

//go:embed rt/*.go rt/*.s
var FS embed.FS
