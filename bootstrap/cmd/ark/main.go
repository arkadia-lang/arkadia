// Command ark is the Arkadia compiler and build tool.
package main

import (
	"fmt"
	"os"

	"github.com/arkadia-lang/arkadia/bootstrap/internal/driver"
	"github.com/arkadia-lang/arkadia/bootstrap/internal/lsp"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "lsp" {
		if err := lsp.Serve(); err != nil {
			fmt.Fprintln(os.Stderr, "ark lsp:", err)
			os.Exit(1)
		}
		return
	}
	os.Exit(driver.CLI(os.Args[1:]))
}
