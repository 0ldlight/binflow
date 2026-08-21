// Command bf-migrate is the BinFlow Artifactory migration tool.
//
// It reads from an Artifactory instance and migrates repositories, packages,
// and artifacts into a BinFlow instance. M6 ticket T-148 establishes the
// skeleton; subsequent tickets wire real subcommands for repository discovery,
// artifact transfer, and incremental sync.
//
// Version is injected at build time via ldflags (same source as binflow-server
// and bf: .goreleaser.yaml or the Makefile release targets).
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

const usage = `bf-migrate is the BinFlow Artifactory migration tool.

Usage:

	bf-migrate <command> [flags]

Commands:

	help      Show this help text

Common flags:

	--help     Show the usage text
	--version  Show the binary version

Environment:

	BINFLOW_SERVER_URL        Base URL of the target BinFlow server (default http://localhost:8080)
	ARTIFACTORY_URL           Base URL of the source Artifactory instance
	ARTIFACTORY_API_KEY       API key for the source Artifactory instance
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(os.Stderr, "bf-migrate: %v\n", err)
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
		fmt.Fprintf(stdout, "bf-migrate %s (%s)\n", version, revision)
		return nil
	default:
		return fmt.Errorf("unknown command %q, see --help for usage", args[0])
	}
}
