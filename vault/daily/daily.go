package daily

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/swiftj/obsidian-mcp/vault"
)

// Vars are the values template variables expand to.
type Vars struct {
	Title      string
	Date       time.Time // for {{date}}
	Time       time.Time // for {{time}}
	DateFormat string    // default for {{date}}; "" is YYYY-MM-DD
	TimeFormat string    // default for {{time}}; "" is HH:mm
}

var varRE = regexp.MustCompile(`(?i)\{\{\s*(title|date|time)\s*(?::([^}]*))?\}\}`)

// Expand expands the template variables of tpl (V-TPL-01): {{title}},
// {{date}}, {{time}}, {{date:FORMAT}} and {{time:FORMAT}}, with Moment.js
// formats. Names ignore case and may have spaces inside the braces. Any
// other {{…}} text and all Templater <% … %> code are copied verbatim and
// never run (V-TPL-02).
func Expand(tpl string, v Vars) (string, error) {
	var ferr error
	out := varRE.ReplaceAllStringFunc(tpl, func(m string) string {
		sm := varRE.FindStringSubmatch(m)
		name, layout := strings.ToLower(sm[1]), strings.TrimSpace(sm[2])
		if name == "title" {
			if sm[2] != "" {
				return m // {{title:…}} is not a variable
			}
			return v.Title
		}
		t := v.Date
		if layout == "" {
			layout = cmpOr(v.DateFormat, "YYYY-MM-DD")
		}
		if name == "time" {
			t = v.Time
			if sm[2] == "" {
				layout = cmpOr(v.TimeFormat, "HH:mm")
			}
		}
		s, err := Format(t, layout)
		if err != nil && ferr == nil {
			ferr = err
		}
		return s
	})
	if ferr != nil {
		return "", ferr
	}
	return out, nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Path returns the daily note's path for date (V-DAILY-01): the folder
// plus the date in the configured format, with ".md". A "/" in the format
// makes subfolders.
func Path(cfg vault.DailyNotesConfig, date time.Time) (string, error) {
	name, err := Format(date, cmpOr(cfg.Format, "YYYY-MM-DD"))
	if err != nil {
		return "", err
	}
	p := path.Join(strings.Trim(cfg.Folder, "/"), name+".md")
	if _, err := vault.Clean(p); err != nil {
		return "", fmt.Errorf("daily: the date format gives the path %q: %w", p, err)
	}
	return p, nil
}

// Note is an opened daily note.
type Note struct {
	File    *vault.File   // the note as it now is (for a dry run, as it would be)
	Created bool          // the note was missing and was (or would be) created
	Result  *vault.Result // the create's result, when Created
}

// Open returns the daily note for date, creating it from the configured
// template if it is missing (V-DAILY-02). The template's {{date}} is date,
// in the daily-note format; {{time}} is now. Any date works.
func Open(ctx context.Context, v *vault.Vault, date, now time.Time, o vault.WriteOptions) (*Note, error) {
	cfg, err := v.Config(ctx)
	if err != nil {
		return nil, err
	}
	p, err := Path(cfg.DailyNotes, date)
	if err != nil {
		return nil, err
	}
	f, err := v.Read(ctx, p)
	if err == nil {
		return &Note{File: f}, nil
	}
	if !errors.As(err, new(*vault.NotFoundError)) {
		return nil, err
	}
	var text string
	if t := cfg.DailyNotes.Template; t != "" {
		tpl, err := v.Read(ctx, withMD(t))
		if err != nil {
			return nil, fmt.Errorf("daily: the daily-note template: %w", err)
		}
		vars := Vars{
			Title: strings.TrimSuffix(path.Base(p), ".md"), Date: date, Time: now,
			DateFormat: cmpOr(cfg.DailyNotes.Format, "YYYY-MM-DD"), TimeFormat: cfg.Templates.TimeFormat,
		}
		if text, err = Expand(string(tpl.Data), vars); err != nil {
			return nil, err
		}
	}
	res, err := v.Create(ctx, p, []byte(text), o)
	if err != nil {
		var ce *vault.ConflictError
		if errors.As(err, &ce) { // created meanwhile
			f, rerr := v.Read(ctx, p)
			if rerr == nil {
				return &Note{File: f}, nil
			}
		}
		return nil, err
	}
	return &Note{File: &vault.File{Path: res.Path, Data: []byte(text), ETag: res.ETag, Kind: vault.KindMarkdown, ModTime: now}, Created: true, Result: res}, nil
}

func withMD(p string) string {
	if path.Ext(p) == "" {
		return p + ".md"
	}
	return p
}

// ErrNoTemplateFolder is returned when the Templates plugin has no folder
// set.
var ErrNoTemplateFolder = errors.New("daily: no template folder is set (Settings → Templates → Template folder location)")

// ListTemplates lists the notes in the template folder, sorted.
func ListTemplates(ctx context.Context, v *vault.Vault) ([]string, error) {
	cfg, err := v.Config(ctx)
	if err != nil {
		return nil, err
	}
	folder := strings.Trim(cfg.Templates.Folder, "/")
	if folder == "" {
		return nil, ErrNoTemplateFolder
	}
	var out []string
	for e, err := range v.List(ctx, folder, true) {
		if err != nil {
			return nil, err
		}
		if e.Kind == vault.KindMarkdown {
			out = append(out, e.Path)
		}
	}
	slices.Sort(out)
	return out, nil
}

// CreateFromTemplate creates the note at p from a template: a vault path,
// or a name inside the template folder. {{title}} is the new note's name,
// and {{date}} and {{time}} are now, in the Templates plugin's formats.
func CreateFromTemplate(ctx context.Context, v *vault.Vault, template, p string, now time.Time, o vault.WriteOptions) (*vault.Result, error) {
	cfg, err := v.Config(ctx)
	if err != nil {
		return nil, err
	}
	tpl, err := v.Read(ctx, withMD(template))
	if errors.As(err, new(*vault.NotFoundError)) && cfg.Templates.Folder != "" {
		tpl, err = v.Read(ctx, withMD(path.Join(strings.Trim(cfg.Templates.Folder, "/"), template)))
	}
	if err != nil {
		return nil, err
	}
	if tpl.Kind != vault.KindMarkdown {
		return nil, &vault.PathError{Op: "template", Path: tpl.Path, Reason: "templates are notes (.md)"}
	}
	text, err := Expand(string(tpl.Data), Vars{
		Title: strings.TrimSuffix(path.Base(p), path.Ext(p)), Date: now, Time: now,
		DateFormat: cfg.Templates.DateFormat, TimeFormat: cfg.Templates.TimeFormat,
	})
	if err != nil {
		return nil, err
	}
	return v.Create(ctx, p, []byte(text), o)
}
