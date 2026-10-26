package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/swiftj/obsidian-mcp/internal/testutil"
)

// syncBuffer is a log sink safe for concurrent writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type harness struct {
	t    *testing.T
	cs   *mcp.ClientSession
	dir  string // the first vault
	logs *syncBuffer
}

// start runs a server over copies of the basic fixture vault and connects
// a client to it.
func start(t *testing.T, readOnly bool, extra ...string) *harness {
	t.Helper()
	dir := testutil.CopyVault(t, "basic")
	cfg := &Config{Vaults: []VaultSpec{{Name: "basic", Path: dir}}, ReadOnly: readOnly, Journal: filepath.Join(t.TempDir(), "journal")}
	for _, e := range extra {
		cfg.Vaults = append(cfg.Vaults, VaultSpec{Name: e, Path: testutil.CopyVault(t, "basic")})
	}
	logs := &syncBuffer{}
	ctx := context.Background()
	s, err := New(ctx, cfg, slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelInfo})), "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.MCP().Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return &harness{t: t, cs: cs, dir: dir, logs: logs}
}

// call calls a tool and returns its text and structured output. It fails
// the test if the call's error state differs from wantErr.
func (h *harness) call(name string, args map[string]any, wantErr bool) (string, map[string]any) {
	h.t.Helper()
	res, err := h.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		h.t.Fatalf("%s: %v", name, err)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	if res.IsError != wantErr {
		h.t.Fatalf("%s(%v): isError = %v; text:\n%s", name, args, res.IsError, text.String())
	}
	var out map[string]any
	if res.StructuredContent != nil {
		b, _ := json.Marshal(res.StructuredContent)
		_ = json.Unmarshal(b, &out)
	}
	return text.String(), out
}

func (h *harness) ok(name string, args map[string]any) (string, map[string]any) {
	h.t.Helper()
	return h.call(name, args, false)
}

func (h *harness) fail(name string, args map[string]any) string {
	h.t.Helper()
	text, _ := h.call(name, args, true)
	return text
}

func (h *harness) write(rel, content string) {
	h.t.Helper()
	p := filepath.Join(h.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

var startRE = regexp.MustCompile(`<<<NOTE-CONTENT nonce=([0-9a-f]{16}) [^\n]*>>>\n`)

// blocks returns the untrusted blocks of a tool's text. Each block ends only
// at the end line carrying its own nonce.
func blocks(t *testing.T, text string) []string {
	t.Helper()
	var out []string
	for {
		m := startRE.FindStringSubmatchIndex(text)
		if m == nil {
			return out
		}
		end := "<<<END NOTE-CONTENT nonce=" + text[m[2]:m[3]] + ">>>\n"
		rest := text[m[1]:]
		k := strings.Index(rest, end)
		if k < 0 {
			t.Fatalf("unterminated block in %q", text)
		}
		out = append(out, rest[:k])
		text = rest[k+len(end):]
	}
}
