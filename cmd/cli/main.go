package main

import (
	"fmt"
	"os"

	"github.com/George-Anagnostou/fridge/cmd/cli/flags"
)

func main() {
	if err := flags.Run(); err != nil {
		fmt.Printf("error running %s: err: %v\n", os.Args[0], err)
		flags.PrintUsage()
		os.Exit(1)
	}
}
