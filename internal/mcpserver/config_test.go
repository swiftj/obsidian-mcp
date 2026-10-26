package mcpserver

import (
	"errors"
	"go/parser"
	"go/token"
	"log/slog"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/swiftj/obsidian-mcp/internal/testutil"
)

func TestParseConfig(t *testing.T) {
	// X-SRV-02
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	c, err := ParseConfig([]string{"/v/Work", "Home=/x/y"}, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Vaults, []VaultSpec{{"Work", "/v/Work"}, {"Home", "/x/y"}}) || c.ReadOnly || c.Journal != "on" || c.LogLevel != slog.LevelInfo || c.Deny != nil {
		t.Errorf("X-SRV-02 defaults = %+v", c)
	}
	e := env(map[string]string{"OBS_MCP_READ_ONLY": "true", "OBS_MCP_DENY": "Private/**, Journal/**", "OBS_MCP_JOURNAL": "off", "OBS_MCP_LOG_LEVEL": "debug"})
	c, err = ParseConfig([]string{"/v"}, e)
	if err != nil {
		t.Fatal(err)
	}
	if !c.ReadOnly || !slices.Equal(c.Deny, []string{"Private/**", "Journal/**"}) || c.Journal != "off" || c.LogLevel != slog.LevelDebug {
		t.Errorf("X-SRV-02 env = %+v", c)
	}
	// Flags win over the environment.
	c, err = ParseConfig([]string{"-read-only=false", "-journal", "/tmp/j", "-log-level", "warn", "-deny", "", "/v"}, e)
	if err != nil {
		t.Fatal(err)
	}
	if c.ReadOnly || c.Journal != "/tmp/j" || c.LogLevel != slog.LevelWarn || c.Deny != nil {
		t.Errorf("X-SRV-02 flags = %+v", c)
	}
	if c, err := ParseConfig([]string{"-read-only", "/v"}, env(nil)); err != nil || !c.ReadOnly {
		t.Errorf("X-SRV-02 bare -read-only = %+v, %v", c, err)
	}
	for _, args := range [][]string{
		{},
		{"/a/V", "/b/v"},
		{"-read-only=maybe", "/v"},
		{"-journal", "relative", "/v"},
		{"-log-level", "loud", "/v"},
		{"-bogus", "/v"},
		{"Name="},
	} {
		if _, err := ParseConfig(args, env(nil)); err == nil {
			t.Errorf("X-SRV-02 %q accepted", args)
		}
	}
	if _, err := ParseConfig([]string{"-h"}, env(nil)); !errors.Is(err, ErrUsage) {
		t.Errorf("-h: %v", err)
	}
}

func TestNoExecOrNetwork(t *testing.T) {
	// X-SRV-08: the server can't run commands or reach the network.
	banned := []string{"os/exec", "net", "net/http", "net/rpc", "plugin"}
	for _, dir := range []string{"internal/mcpserver", "cmd/obs-mcp", "vault", "vault/links", "vault/search", "vault/ofm", "vault/props", "internal/scan"} {
		files, _ := filepath.Glob(filepath.Join(testutil.RepoRoot(), dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			af, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range af.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				if slices.Contains(banned, p) {
					t.Errorf("X-SRV-08 %s imports %s", f, p)
				}
			}
		}
	}
	for _, name := range slices.Concat(readTools, writeTools) {
		for _, bad := range []string{"exec", "eval", "command", "uri", "url", "fetch", "plugin", "template"} {
			if strings.Contains(name, bad) {
				t.Errorf("X-SRV-08 tool %s", name)
			}
		}
	}
}
