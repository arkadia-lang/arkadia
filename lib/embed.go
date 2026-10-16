// Package lib embeds the Arkadia standard library sources.
package lib

import "embed"

// FS holds Prelude.ark and the Std.* modules.
//
//go:embed *.ark Std
var FS embed.FS
