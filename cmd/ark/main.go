package main

import (
	"fmt"
	"os"

	"github.com/verniyyy/arkadia/internal/driver"
)

func main() {
	os.Exit(driver.CLI(os.Args[1:]))
}

var _ = fmt.Sprint
