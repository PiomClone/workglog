package main

import (
	"fmt"
	"os"

	"github.com/PiomClone/workglog/internal/app"
)

var (
	version = "dev"
	commit  = "none"
	builtAt = "unknown"
)

func main() {
	app.SetVersion(version, commit, builtAt)
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
