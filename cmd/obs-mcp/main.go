// Command obs-mcp is an MCP server for Obsidian vaults, over stdio.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/swiftj/obsidian-mcp/internal/mcpserver"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run serves MCP on stdin and stdout until stdin closes or ctx ends. Only
// the protocol goes to stdout; logs and errors go to stderr (X-SRV-01).
func run(ctx context.Context, args []string, getenv func(string) string, stdin io.ReadCloser, stdout io.WriteCloser, stderr io.Writer) int {
	for _, a := range args {
		if a == "-version" || a == "--version" {
			_, _ = fmt.Fprintln(stdout, "obs-mcp", version)
			return 0
		}
	}
	cfg, err := mcpserver.ParseConfig(args, getenv)
	if errors.Is(err, mcpserver.ErrUsage) {
		_, _ = fmt.Fprint(stderr, mcpserver.Usage)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "obs-mcp: %v\n\n%s", err, mcpserver.Usage)
		return 2
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	s, err := mcpserver.New(ctx, cfg, log, version)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "obs-mcp: %v\n", err)
		return 1
	}
	defer func() { _ = s.Close() }()
	log.Info("obs-mcp started", "version", version, "vaults", len(cfg.Vaults), "read_only", cfg.ReadOnly)
	err = s.MCP().Run(ctx, &mcp.IOTransport{Reader: stdin, Writer: stdout})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
		log.Error("obs-mcp stopped", "err", err)
		return 1
	}
	return 0
}
