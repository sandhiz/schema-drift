package main

import (
	"os"

	"schema-drift/internal/cli"
)

func main() {
	_, _ = cli.LoadDefaultDotEnv()
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
