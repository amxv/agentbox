package main

import (
	"os"

	"agentbox/internal/agentbox/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
