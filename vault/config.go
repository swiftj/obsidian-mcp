package vault

import (
	"context"
	"encoding/json"
	"fmt"
)

// Config holds the Obsidian settings the vault library uses. Missing files
// and keys take Obsidian's defaults (DECISIONS D-006).
type Config struct {
	NewLinkFormat        string // "shortest", "relative" or "absolute"
	UseMarkdownLinks     bool
	AlwaysUpdateLinks    bool
	NewFileLocation      string // "root", "current" or "folder"
	NewFileFolderPath    string
	AttachmentFolderPath string
	UserIgnoreFilters    []string
	// CorePlugins maps core plugin ids to whether they are on. It is nil when
	// core-plugins.json is missing.
	CorePlugins map[string]bool
	DailyNotes  DailyNotesConfig
	Templates   TemplatesConfig
}

// DailyNotesConfig is daily-notes.json.
type DailyNotesConfig struct {
	Folder   string // "" is the vault root
	Format   string // moment.js pattern
	Template string // template note path without .md; "" for none
}

// TemplatesConfig is templates.json.
type TemplatesConfig struct {
	Folder     string
	DateFormat string
	TimeFormat string
}

// DefaultConfig returns the settings of a vault whose config folder is
// missing or empty (V-CFG-03).
func DefaultConfig() *Config {
	return &Config{
		NewLinkFormat:        "shortest",
		NewFileLocation:      "root",
		NewFileFolderPath:    "/",
		AttachmentFolderPath: "/",
		DailyNotes:           DailyNotesConfig{Format: "YYYY-MM-DD"},
		Templates:            TemplatesConfig{DateFormat: "YYYY-MM-DD", TimeFormat: "HH:mm"},
	}
}

// Config reads the vault's Obsidian settings (V-CFG-01, V-CFG-02). The config
// folder is read directly; it stays invisible to every other operation.
func (v *Vault) Config(ctx context.Context) (*Config, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c := DefaultConfig()

	var app struct {
		NewLinkFormat        *string  `json:"newLinkFormat"`
		UseMarkdownLinks     *bool    `json:"useMarkdownLinks"`
		AlwaysUpdateLinks    *bool    `json:"alwaysUpdateLinks"`
		NewFileLocation      *string  `json:"newFileLocation"`
		NewFileFolderPath    *string  `json:"newFileFolderPath"`
		AttachmentFolderPath *string  `json:"attachmentFolderPath"`
		UserIgnoreFilters    []string `json:"userIgnoreFilters"`
	}
	if err := v.readConfigJSON("app.json", &app); err != nil {
		return nil, err
	}
	setStr(&c.NewLinkFormat, app.NewLinkFormat)
	setBool(&c.UseMarkdownLinks, app.UseMarkdownLinks)
	setBool(&c.AlwaysUpdateLinks, app.AlwaysUpdateLinks)
	setStr(&c.NewFileLocation, app.NewFileLocation)
	setStr(&c.NewFileFolderPath, app.NewFileFolderPath)
	setStr(&c.AttachmentFolderPath, app.AttachmentFolderPath)
	c.UserIgnoreFilters = app.UserIgnoreFilters

	if err := v.readConfigJSON("core-plugins.json", &c.CorePlugins); err != nil {
		return nil, err
	}

	var daily struct {
		Folder   *string `json:"folder"`
		Format   *string `json:"format"`
		Template *string `json:"template"`
	}
	if err := v.readConfigJSON("daily-notes.json", &daily); err != nil {
		return nil, err
	}
	setStr(&c.DailyNotes.Folder, daily.Folder)
	setStr(&c.DailyNotes.Format, daily.Format)
	setStr(&c.DailyNotes.Template, daily.Template)

	var tpl struct {
		Folder     *string `json:"folder"`
		DateFormat *string `json:"dateFormat"`
		TimeFormat *string `json:"timeFormat"`
	}
	if err := v.readConfigJSON("templates.json", &tpl); err != nil {
		return nil, err
	}
	setStr(&c.Templates.Folder, tpl.Folder)
	setStr(&c.Templates.DateFormat, tpl.DateFormat)
	setStr(&c.Templates.TimeFormat, tpl.TimeFormat)
	return c, nil
}

// readConfigJSON decodes <config folder>/name into dst. A missing file leaves
// dst untouched.
func (v *Vault) readConfigJSON(name string, dst any) error {
	rel := v.pol.configDir + "/" + name
	data, _, err := v.readRegular("config", rel)
	if isNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("vault: %s: %w", rel, err)
	}
	return nil
}

// setStr and setBool copy a present, non-empty setting over its default.
func setStr(dst *string, v *string) {
	if v != nil && *v != "" {
		*dst = *v
	}
}

func setBool(dst *bool, v *bool) {
	if v != nil {
		*dst = *v
	}
}
