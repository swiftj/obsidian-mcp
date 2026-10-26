// Package mcpserver is the obs-mcp MCP server: task-shaped tools over the
// vault library.
package mcpserver

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is the server's configuration (X-SRV-02).
type Config struct {
	Vaults   []VaultSpec
	ReadOnly bool
	Deny     []string
	// Journal is "on" (the default folder), "off", or a folder path.
	Journal  string
	LogLevel slog.Level
}

// VaultSpec is one vault: its tool-facing name and its folder.
type VaultSpec struct {
	Name string
	Path string
}

// ErrUsage is returned for -h and -help.
var ErrUsage = errors.New("usage")

// Usage describes the command line.
const Usage = `usage: obs-mcp [flags] VAULT [VAULT...]

VAULT is a vault folder, or NAME=FOLDER to name it. A vault is otherwise
named by its folder. The first vault is the default for tools.

Flags override the environment variables in brackets:
  -read-only        register no write tools [OBS_MCP_READ_ONLY]
  -deny GLOBS       comma-separated globs hidden from every tool [OBS_MCP_DENY]
  -journal VALUE    on, off, or a folder for undo history [OBS_MCP_JOURNAL] (default on)
  -log-level LEVEL  debug, info, warn or error [OBS_MCP_LOG_LEVEL] (default info)
  -version          print the version and exit
`

// ParseConfig reads the configuration from args (without the program name)
// and the environment. Flags win over environment variables.
func ParseConfig(args []string, getenv func(string) string) (*Config, error) {
	fs := flag.NewFlagSet("obs-mcp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	readOnly := fs.String("read-only", getenv("OBS_MCP_READ_ONLY"), "")
	deny := fs.String("deny", getenv("OBS_MCP_DENY"), "")
	journal := fs.String("journal", getenv("OBS_MCP_JOURNAL"), "")
	level := fs.String("log-level", getenv("OBS_MCP_LOG_LEVEL"), "")
	if err := fs.Parse(boolFlag(args, "read-only")); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, ErrUsage
		}
		return nil, err
	}
	c := &Config{Journal: "on"}
	if *readOnly != "" {
		b, err := strconv.ParseBool(*readOnly)
		if err != nil {
			return nil, fmt.Errorf("read-only: %q is not true or false", *readOnly)
		}
		c.ReadOnly = b
	}
	for g := range strings.SplitSeq(*deny, ",") {
		if g = strings.TrimSpace(g); g != "" {
			c.Deny = append(c.Deny, g)
		}
	}
	switch j := strings.TrimSpace(*journal); strings.ToLower(j) {
	case "", "on", "true":
		c.Journal = "on"
	case "off", "false":
		c.Journal = "off"
	default:
		if !filepath.IsAbs(j) {
			return nil, fmt.Errorf("journal: %q must be on, off or an absolute folder path", j)
		}
		c.Journal = j
	}
	if *level != "" {
		if err := c.LogLevel.UnmarshalText([]byte(*level)); err != nil {
			return nil, fmt.Errorf("log-level: %q is not debug, info, warn or error", *level)
		}
	}
	seen := map[string]bool{}
	for _, a := range fs.Args() {
		spec := VaultSpec{Path: a}
		if name, p, ok := strings.Cut(a, "="); ok && name != "" && !strings.ContainsAny(name, `/\`) {
			spec = VaultSpec{Name: name, Path: p}
		}
		if spec.Path == "" {
			return nil, fmt.Errorf("vault %q has no folder", a)
		}
		if spec.Name == "" {
			spec.Name = filepath.Base(filepath.Clean(spec.Path))
		}
		key := strings.ToLower(spec.Name)
		if seen[key] {
			return nil, fmt.Errorf("two vaults are named %q; name one with NAME=FOLDER", spec.Name)
		}
		seen[key] = true
		c.Vaults = append(c.Vaults, spec)
	}
	if len(c.Vaults) == 0 {
		return nil, errors.New("no vault given")
	}
	return c, nil
}

// boolFlag lets "-name" stand alone for a string flag by rewriting it to
// "-name=true".
func boolFlag(args []string, name string) []string {
	out := make([]string, 0, len(args))
	for i, a := range args {
		if a == "--" {
			return append(out, args[i:]...)
		}
		if a == "-"+name || a == "--"+name {
			a += "=true"
		}
		out = append(out, a)
	}
	return out
}
