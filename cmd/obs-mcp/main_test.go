package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/swiftj/obsidian-mcp/internal/testutil"
)

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

func TestStdoutIsProtocolOnly(t *testing.T) {
	// X-SRV-01: stdout carries only JSON-RPC; logs go to stderr.
	dir := testutil.CopyVault(t, "basic")
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	var stderr syncBuffer
	env := map[string]string{"OBS_MCP_JOURNAL": filepath.Join(t.TempDir(), "j")}
	done := make(chan int)
	go func() {
		done <- run(context.Background(), []string{dir}, func(k string) string { return env[k] }, inR, outW, &stderr)
		_ = outW.Close()
	}()
	sc := bufio.NewScanner(outR)
	sc.Buffer(nil, 1<<20)
	for _, req := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_note","arguments":{"path":"Welcome.md"}}}`,
	} {
		if _, err := io.WriteString(inW, req+"\n"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(req, `"id"`) {
			continue
		}
		if !sc.Scan() {
			t.Fatalf("no response to %s; stderr:\n%s", req, stderr.String())
		}
		var msg struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil || msg.JSONRPC != "2.0" || len(msg.ID) == 0 {
			t.Fatalf("X-SRV-01 non-protocol line on stdout: %q", sc.Text())
		}
	}
	_ = inW.Close()
	for sc.Scan() {
		t.Errorf("X-SRV-01 extra stdout: %q", sc.Text())
	}
	if code := <-done; code != 0 {
		t.Errorf("exit %d; stderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "tool=read_note") {
		t.Errorf("X-SRV-01 stderr:\n%s", stderr.String())
	}
}

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

func TestBadConfig(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), nil, func(string) string { return "" }, io.NopCloser(strings.NewReader("")), nopCloser{&stdout}, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "no vault") {
		t.Errorf("no vault: exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	stderr.Reset()
	if code := run(context.Background(), []string{"/does/not/exist"}, func(k string) string { return map[string]string{"OBS_MCP_JOURNAL": "off"}[k] }, io.NopCloser(strings.NewReader("")), nopCloser{&stdout}, &stderr); code != 1 || stdout.Len() != 0 {
		t.Errorf("missing folder: exit %d, stderr %q", code, stderr.String())
	}
	stdout.Reset()
	if code := run(context.Background(), []string{"--version"}, nil, nil, nopCloser{&stdout}, &stderr); code != 0 || !strings.HasPrefix(stdout.String(), "obs-mcp ") {
		t.Errorf("version: %d %q", code, stdout.String())
	}
}
