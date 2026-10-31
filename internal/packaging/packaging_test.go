// Package packaging tests the Desktop extension manifest, the Cowork
// plugin and its skills against the server (X-PLUG-01..09).
package packaging

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/swiftj/obsidian-mcp/internal/mcpserver"
	"github.com/swiftj/obsidian-mcp/internal/testutil"
)

type userConfig struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Multiple    bool   `json:"multiple"`
	Default     any    `json:"default"`
}

type manifest struct {
	ManifestVersion string `json:"manifest_version"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	Server          struct {
		Type       string `json:"type"`
		EntryPoint string `json:"entry_point"`
		MCPConfig  struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcp_config"`
	} `json:"server"`
	Tools []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"tools"`
	UserConfig    map[string]*userConfig `json:"user_config"`
	Compatibility struct {
		Platforms []string `json:"platforms"`
	} `json:"compatibility"`
}

func repo(parts ...string) string {
	return filepath.Join(append([]string{testutil.RepoRoot()}, parts...)...)
}

func loadManifest(t *testing.T) *manifest {
	t.Helper()
	b, err := os.ReadFile(repo("plugin", "mcpb", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return &m
}

// serverTools lists the tools of a full (not read-only) server.
func serverTools(t *testing.T) []*mcp.Tool {
	t.Helper()
	ctx := context.Background()
	cfg := &mcpserver.Config{Vaults: []mcpserver.VaultSpec{{Name: "basic", Path: testutil.CopyVault(t, "basic")}}, Journal: "off"}
	s, err := mcpserver.New(ctx, cfg, nil, "test")
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
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res.Tools
}

func TestManifest(t *testing.T) {
	// X-PLUG-01, X-PLUG-02
	m := loadManifest(t)
	mc := m.Server.MCPConfig
	if m.ManifestVersion != "0.3" || m.Server.Type != "binary" || m.Server.EntryPoint != "server/obs-mcp" ||
		mc.Command != "${__dirname}/server/obs-mcp" || !slices.Equal(m.Compatibility.Platforms, []string{"darwin"}) {
		t.Errorf("X-PLUG-01 manifest = %+v", m)
	}
	v := m.UserConfig["vaults"]
	if v == nil || v.Type != "directory" || !v.Multiple || !v.Required {
		t.Errorf("X-PLUG-02 vaults = %+v", v)
	}
	for key, want := range map[string]struct {
		typ string
		def any
	}{"read_only": {"boolean", false}, "deny": {"string", ""}, "journal": {"boolean", true}} {
		u := m.UserConfig[key]
		if u == nil || u.Type != want.typ || u.Required || u.Default != want.def {
			t.Errorf("X-PLUG-02 %s = %+v", key, u)
		}
	}
	if keys := slices.Sorted(maps.Keys(m.UserConfig)); !slices.Equal(keys, []string{"deny", "journal", "read_only", "vaults"}) {
		t.Errorf("X-PLUG-02 user_config keys = %v", keys)
	}
	if errs := checkManifest(m); len(errs) > 0 {
		t.Errorf("X-PLUG-02, X-PLUG-04 manifest: %v", errs)
	}
}

// serverEnv are the environment variables the server reads.
var serverEnv = []string{"OBS_MCP_READ_ONLY", "OBS_MCP_DENY", "OBS_MCP_JOURNAL", "OBS_MCP_LOG_LEVEL"}

// checkManifest checks the rules a manifest must keep for Desktop's
// substitution to give the server a working command line.
func checkManifest(m *manifest) []string {
	var errs []string
	mc := m.Server.MCPConfig
	all := mc.Command + strings.Join(mc.Args, " ")
	for k, v := range mc.Env {
		if !slices.Contains(serverEnv, k) {
			errs = append(errs, "env "+k+" is not read by the server")
		}
		all += v
	}
	for k, u := range m.UserConfig {
		ref := "${user_config." + k + "}"
		if !strings.Contains(all, ref) {
			errs = append(errs, k+" is not used in mcp_config")
		}
		if !u.Required && u.Default == nil {
			errs = append(errs, k+" is optional without a default, so Desktop would pass "+ref+" literally")
		}
		if u.Multiple && !slices.Contains(mc.Args, ref) {
			errs = append(errs, k+" is multiple but not a whole args element, so it would not expand")
		}
	}
	return errs
}

// substitute re-implements Claude Desktop's getMcpConfigForManifest
// substitution (X-PLUG-03, CLAUDE.md §Packaging): ${__dirname} is the
// extension folder; a user_config value replaces ${user_config.key}, with
// booleans as "true"/"false"; an array expands into several args only when
// an args element is exactly the reference; and an optional setting with
// no value and no default is left as the literal reference.
func substitute(m *manifest, values map[string]any, dir string) (string, []string, map[string]string) {
	value := func(k string) (any, bool) {
		if v, ok := values[k]; ok {
			return v, true
		}
		if u := m.UserConfig[k]; u != nil && u.Default != nil {
			return u.Default, true
		}
		return nil, false
	}
	refRE := regexp.MustCompile(`\$\{user_config\.([A-Za-z0-9_]+)\}`)
	str := func(s string) string {
		s = strings.ReplaceAll(s, "${__dirname}", dir)
		return refRE.ReplaceAllStringFunc(s, func(ref string) string {
			v, ok := value(refRE.FindStringSubmatch(ref)[1])
			if !ok {
				return ref
			}
			switch x := v.(type) {
			case bool:
				return fmt.Sprint(x)
			case []string:
				return strings.Join(x, ",")
			default:
				return fmt.Sprint(x)
			}
		})
	}
	mc := m.Server.MCPConfig
	var args []string
	for _, a := range mc.Args {
		if sub := refRE.FindStringSubmatch(a); sub != nil && sub[0] == a {
			if v, ok := value(sub[1]); ok {
				if list, isList := v.([]string); isList {
					args = append(args, list...)
					continue
				}
			}
		}
		args = append(args, str(a))
	}
	env := map[string]string{}
	for k, v := range mc.Env {
		env[k] = str(v)
	}
	return str(mc.Command), args, env
}

func parse(t *testing.T, m *manifest, values map[string]any) (*mcpserver.Config, string, error) {
	t.Helper()
	cmd, args, env := substitute(m, values, "/Ext")
	cfg, err := mcpserver.ParseConfig(args, func(k string) string { return env[k] })
	return cfg, cmd, err
}

func TestDesktopSubstitution(t *testing.T) {
	// X-PLUG-03
	m := loadManifest(t)
	cfg, cmd, err := parse(t, m, map[string]any{
		"vaults":    []string{"/Users/me/Notes", "/Users/me/-Work"},
		"read_only": true, "deny": "Journal/**, Private/**", "journal": false,
	})
	if err != nil || cmd != "/Ext/server/obs-mcp" {
		t.Fatalf("X-PLUG-03 cmd %q: %v", cmd, err)
	}
	want := []mcpserver.VaultSpec{{Name: "Notes", Path: "/Users/me/Notes"}, {Name: "-Work", Path: "/Users/me/-Work"}}
	if !slices.Equal(cfg.Vaults, want) || !cfg.ReadOnly || !slices.Equal(cfg.Deny, []string{"Journal/**", "Private/**"}) || cfg.Journal != "off" {
		t.Errorf("X-PLUG-03 config = %+v", cfg)
	}
	cfg, _, err = parse(t, m, map[string]any{"vaults": []string{"/v"}})
	if err != nil || cfg.ReadOnly || cfg.Deny != nil || cfg.Journal != "on" || len(cfg.Vaults) != 1 {
		t.Errorf("X-PLUG-03 defaults = %+v, %v", cfg, err)
	}

	// Broken manifests are caught, and show why the rules exist.
	noDefault := loadManifest(t)
	noDefault.UserConfig["read_only"].Default = nil
	if _, _, err := parse(t, noDefault, map[string]any{"vaults": []string{"/v"}}); err == nil || !strings.Contains(err.Error(), "${user_config.read_only}") {
		t.Errorf("a literal reference should fail ParseConfig: %v", err)
	}
	inEnv := loadManifest(t)
	inEnv.Server.MCPConfig.Args = []string{"--"}
	inEnv.Server.MCPConfig.Env["OBS_MCP_VAULTS"] = "${user_config.vaults}"
	if _, _, err := parse(t, inEnv, map[string]any{"vaults": []string{"/a", "/b"}}); err == nil {
		t.Error("vaults in env should leave the server without vaults")
	}
	errs := strings.Join(append(checkManifest(noDefault), checkManifest(inEnv)...), "\n")
	for _, want := range []string{"read_only is optional without a default", "env OBS_MCP_VAULTS is not read", "vaults is multiple but not a whole args element"} {
		if !strings.Contains(errs, want) {
			t.Errorf("checkManifest missed %q in:\n%s", want, errs)
		}
	}
}

func TestManifestTools(t *testing.T) {
	// X-PLUG-04
	m := loadManifest(t)
	var listed []string
	for _, tl := range m.Tools {
		listed = append(listed, tl.Name)
		if tl.Description == "" {
			t.Errorf("X-PLUG-04 %s has no description", tl.Name)
		}
	}
	var served []string
	for _, tl := range serverTools(t) {
		served = append(served, tl.Name)
	}
	slices.Sort(listed)
	slices.Sort(served)
	if !slices.Equal(listed, served) {
		t.Errorf("X-PLUG-04 manifest tools %v\n != server tools %v", listed, served)
	}
}

func readPlugin(t *testing.T) (version string) {
	t.Helper()
	b, err := os.ReadFile(repo("plugin", "cowork", ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(b, &p); err != nil || p.Name == "" {
		t.Fatalf("plugin.json: %v %+v", err, p)
	}
	return p.Version
}

func TestPlugin(t *testing.T) {
	// X-PLUG-05, X-PLUG-07
	if v, m := readPlugin(t), loadManifest(t).Version; v != m || v == "" {
		t.Errorf("X-PLUG-05 plugin version %q, manifest version %q", v, m)
	}
	if _, err := os.Stat(repo("plugin", "cowork", "bin")); !os.IsNotExist(err) {
		t.Errorf("X-PLUG-07 plugin/cowork/bin exists: %v", err)
	}
}

type skill struct {
	folder, name, description string
	lines                     int
	body                      string
}

func readSkills(t *testing.T) []skill {
	t.Helper()
	dirs, err := os.ReadDir(repo("plugin", "cowork", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	var out []skill
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		b, err := os.ReadFile(repo("plugin", "cowork", "skills", d.Name(), "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		s := skill{folder: d.Name(), body: string(b), lines: strings.Count(string(b), "\n")}
		front, ok := strings.CutPrefix(string(b), "---\n")
		front, _, ok2 := strings.Cut(front, "\n---\n")
		if !ok || !ok2 {
			t.Fatalf("%s: no frontmatter", d.Name())
		}
		sc := bufio.NewScanner(strings.NewReader(front))
		for sc.Scan() {
			k, v, _ := strings.Cut(sc.Text(), ": ")
			switch k {
			case "name":
				s.name = v
			case "description":
				s.description = v
			}
		}
		out = append(out, s)
	}
	if len(out) != 3 {
		t.Fatalf("skills = %d; want 3", len(out))
	}
	return out
}

func TestSkillFormat(t *testing.T) {
	// X-PLUG-08
	nameRE := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	for _, s := range readSkills(t) {
		if s.lines > 200 {
			t.Errorf("X-PLUG-08 %s has %d lines", s.folder, s.lines)
		}
		if s.name != s.folder || !nameRE.MatchString(s.name) || len(s.name) > 64 {
			t.Errorf("X-PLUG-08 %s name = %q", s.folder, s.name)
		}
		if s.description == "" || len(s.description) > 1024 {
			t.Errorf("X-PLUG-08 %s description has %d characters", s.folder, len(s.description))
		}
	}
}

func TestSkillToolDrift(t *testing.T) {
	// X-PLUG-06: every snake_case word a skill writes as code must be
	// something the server knows: a tool, an argument or output field, or
	// a value its descriptions name (such as an edit op).
	b, err := json.Marshal(serverTools(t))
	if err != nil {
		t.Fatal(err)
	}
	snake := regexp.MustCompile(`\b[a-z][a-z0-9]*(?:_[a-z0-9]+)+\b`)
	known := map[string]bool{}
	for _, w := range snake.FindAllString(string(b), -1) {
		known[w] = true
	}
	tools := map[string]bool{}
	for _, tl := range serverTools(t) {
		tools[tl.Name] = true
	}
	fence := regexp.MustCompile("(?s)````.*?````|```.*?```")
	code := regexp.MustCompile("`([^`\n]+)`")
	for _, s := range readSkills(t) {
		used := 0
		for _, c := range code.FindAllStringSubmatch(fence.ReplaceAllString(s.body, ""), -1) {
			for _, w := range snake.FindAllString(c[1], -1) {
				if !known[w] {
					t.Errorf("X-PLUG-06 %s names %q, which the server doesn't have", s.folder, w)
				}
				if tools[w] {
					used++
				}
			}
		}
		if used == 0 {
			t.Errorf("X-PLUG-06 %s names no tools", s.folder)
		}
	}
}

func TestMakefile(t *testing.T) {
	// X-PLUG-09: only the darwin/arm64 extension is built, and
	// verify-static checks the binary inside it.
	b, err := os.ReadFile(repo("Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	mk := string(b)
	if got := regexp.MustCompile(`[\w$()/-]*\.mcpb\b`).FindAllString(mk, -1); len(got) == 0 {
		t.Fatal("X-PLUG-09 the Makefile builds no .mcpb")
	} else {
		for _, f := range got {
			if f != "$(DIST)/obs-mcp-darwin-arm64.mcpb" {
				t.Errorf("X-PLUG-09 unexpected .mcpb %q", f)
			}
		}
	}
	vs := mk[strings.Index(mk, "verify-static:"):]
	vs = vs[:strings.Index(vs, "\n\n")]
	for _, want := range []string{"mcpb", "unzip", "CGO_ENABLED=0", "Mach-O 64-bit executable arm64", "cmp -s"} {
		if !strings.Contains(vs, want) {
			t.Errorf("X-PLUG-09 verify-static lacks %q", want)
		}
	}
}
