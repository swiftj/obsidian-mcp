package vault

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestConfigFromFiles(t *testing.T) {
	// V-CFG-01, V-CFG-02: the fixture holds the D-006 sample app.json and
	// plugin option files.
	v, _ := openBasic(t, Options{})
	c, err := v.Config(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := &Config{
		NewLinkFormat:        "shortest",
		UseMarkdownLinks:     false,
		AlwaysUpdateLinks:    true,
		NewFileLocation:      "folder",
		NewFileFolderPath:    "Inbox",
		AttachmentFolderPath: "/",
		UserIgnoreFilters:    []string{"Archive/"},
		CorePlugins:          map[string]bool{"daily-notes": true, "templates": false},
		DailyNotes:           DailyNotesConfig{Folder: "Daily", Format: "YYYY/MM/YYYY-MM-DD", Template: "Templates/Daily"},
		Templates:            TemplatesConfig{Folder: "Templates", DateFormat: "DD.MM.YYYY", TimeFormat: "HH:mm"},
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("V-CFG-01 config =\n%+v\nwant\n%+v", c, want)
	}
}

func TestConfigDefaults(t *testing.T) {
	// V-CFG-03: a missing config folder, or app.json "{}", gives the defaults.
	ctx := context.Background()
	for _, setup := range []func(dir string) error{
		func(dir string) error { return os.RemoveAll(filepath.Join(dir, ".obsidian")) },
		func(dir string) error {
			if err := os.RemoveAll(filepath.Join(dir, ".obsidian")); err != nil {
				return err
			}
			if err := os.Mkdir(filepath.Join(dir, ".obsidian"), 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, ".obsidian", "app.json"), []byte("{}"), 0o644)
		},
	} {
		v, dir := openBasic(t, Options{})
		if err := setup(dir); err != nil {
			t.Fatal(err)
		}
		c, err := v.Config(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(c, DefaultConfig()) {
			t.Errorf("V-CFG-03 config = %+v; want defaults", c)
		}
	}
	d := DefaultConfig()
	if d.NewLinkFormat != "shortest" || d.UseMarkdownLinks || d.AlwaysUpdateLinks || d.NewFileLocation != "root" ||
		d.NewFileFolderPath != "/" || d.AttachmentFolderPath != "/" || d.UserIgnoreFilters != nil {
		t.Errorf("V-CFG-01 defaults differ from D-006: %+v", d)
	}
}

func TestConfigOverrideAndErrors(t *testing.T) {
	ctx := context.Background()
	// V-PATH-08: an overridden config folder is read instead of .obsidian.
	v, dir := openBasic(t, Options{ConfigDir: ".cfg"})
	if err := os.Mkdir(filepath.Join(dir, ".cfg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".cfg", "app.json"), []byte(`{"newLinkFormat":"relative"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := v.Config(ctx)
	if err != nil || c.NewLinkFormat != "relative" || c.AlwaysUpdateLinks {
		t.Errorf("V-PATH-08 config from .cfg = %+v, %v", c, err)
	}
	// V-CFG-01: a broken app.json is reported, not silently defaulted.
	if err := os.WriteFile(filepath.Join(dir, ".cfg", "app.json"), []byte(`{`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Config(ctx); err == nil {
		t.Error("V-CFG-01 broken app.json accepted")
	}
	// The config folder stays unreadable through the normal API.
	_, err = v.Read(ctx, ".cfg/app.json")
	asPathError(t, err, "V-PATH-07 read config")
}
