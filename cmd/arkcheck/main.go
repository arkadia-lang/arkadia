package main

import (
	"fmt"
	"os"

	"github.com/verniyyy/arkadia/internal/driver"
	"github.com/verniyyy/arkadia/internal/types"
)

func main() {
	l := driver.NewLoader(nil)
	p, err := l.LoadFile(os.Args[1])
	if err != nil {
		fmt.Print(l.Sources.Render(err, false))
		os.Exit(1)
	}
	for _, m := range p.Modules {
		if m.Name == p.Main {
			for n, v := range p.Global.Modules[m.Name].Values {
				if v.Scheme != nil {
					fmt.Printf("%s : %s\n", n, types.SchemeString(v.Scheme))
				}
			}
		}
	}
}
