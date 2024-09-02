package main

import (
	"fmt"
	"os"
)

func main() {
	if err := Run(); err != nil {
		fmt.Printf("error running %s: err: %v\n", os.Args[0], err)
		PrintUsage()
		os.Exit(1)
	}
}
