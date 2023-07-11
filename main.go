package main

import (
	"github.com/asmild/artifactory-cleaner/cmd"
	"os"
)

func main() {
	if err := cmd.Execute(); nil != err {
		os.Exit(1)
	}
}
