// Command obs-mcp is an MCP server for Obsidian vaults.
//
// Phase 0 stub: it only reports its version.
package main

import (
	"fmt"
	"os"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if _, err := fmt.Fprintln(os.Stdout, "obs-mcp", version); err != nil {
		os.Exit(1)
	}
}
