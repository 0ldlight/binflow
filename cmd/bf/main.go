// Command bf is the BinFlow CLI client tool.
//
// It provides a command-line interface for common BinFlow operations:
// repository management, artifact upload/download, and system administration.
// M6 ticket T-148 establishes the skeleton; subsequent tickets wire real
// subcommands.
//
// Version is injected at build time via ldflags (same source as binflow-server
// and bf-migrate: .goreleaser.yaml or the Makefile release targets).
// A bare build leaves the honest dev placeholder in place.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// version and revision are stamped at build time by the release faces
// (goreleaser ldflags -X main.version/-X main.revision, T-127/FR-34, T-148).
// A bare build leaves the dev placeholder.
var (
	version  = "dev"
	revision = "dev"
)

const usage = `bf is the BinFlow CLI client.

Usage:

	bf <command> [flags]

Commands:

	help      Show this help text

Common flags:

	--help     Show the usage text
	--version  Show the binary version

Environment:

	BINFLOW_SERVER_URL  Base URL of the BinFlow server (default http://localhost:8080)
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(os.Stderr, "bf: %v\n", err)
		}
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return nil
	}

	switch args[0] {
	case "--help", "-h", "help":
		fmt.Fprint(stdout, usage)
		return nil
	case "--version":
		fmt.Fprintf(stdout, "bf %s (%s)\n", version, revision)
		return nil
	default:
		return fmt.Errorf("unknown command %q, see --help for usage", args[0])
	}
}
