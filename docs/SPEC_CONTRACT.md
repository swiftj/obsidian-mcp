# SPEC_CONTRACT

This file lists numbered, testable requirements. Every requirement has at least one `_test.go` file that names its ID. `internal/speccov` fails the build when a requirement whose phase is at or below the current phase (`docs/PHASE`) has no such test.

## Format

Each requirement is a single line:

```
- **ID** `pN` statement. ⟨source⟩ [probed D-006]
```

| Part | Meaning |
|---|---|
| `ID` | `<GROUP>-<AREA>-<NN>` |
| `pN` | the phase that must cover it |
| `⟨source⟩` | a file in `docs/` with a section, or `CLAUDE.md §section` for our own design rules |
| `[probed D-006]` | the statement rests on behaviour observed in Obsidian and recorded in DECISIONS. |
| `[assumed D-006]` | Obsidian's behaviour was not probed; the statement follows the docs and existing servers, and can be revisited. |
| `[policy]` | our own choice where Obsidian doesn't define the behaviour. The owner can override it. |

**Groups:**

| Group | Area |
|---|---|
| `M` | meta (spec, sources) |
| `V` | vault, sandbox, I/O, search, daily notes, templates |
| `O` | OFM scanner |
| `L` | links |
| `P` | properties |
| `C` | canvas |
| `B` | bases |
| `X` | MCP server |
| `X-PLUG` | packaging |

---

## M: Meta

- **M-SPEC-01** `p0` Every requirement ID in this file is well-formed and unique, and has a phase tag. ⟨CLAUDE.md §Sources of truth⟩
- **M-SPEC-02** `p0` Every requirement whose phase is ≤ `docs/PHASE` is named by at least one `_test.go` file. ⟨CLAUDE.md §Sources of truth⟩
- **M-SRC-01** `p0` Every file listed in `docs/SHA256SUMS` exists and matches its recorded sha256. ⟨CLAUDE.md §Sources of truth⟩

## V: Vault core

### Path sandbox
- **V-PATH-01** `p1` Every path is vault-relative and uses `/`. Absolute paths are rejected. ⟨CLAUDE.md §1⟩
- **V-PATH-02** `p1` Empty segments, `.` and `..` are rejected. ⟨CLAUDE.md §1⟩
- **V-PATH-03** `p1` NUL, backslash, and any `%` followed by two hex digits are rejected. Paths are never URL-decoded. ⟨CLAUDE.md §1⟩
- **V-PATH-04** `p1` A segment ending in `.` or space is rejected. ⟨CLAUDE.md §1⟩
- **V-PATH-05** `p1` A symlink in any path component is refused (checked with `Lstat`). Files are opened with `O_NOFOLLOW`. ⟨CLAUDE.md §1⟩
- **V-PATH-06** `p1` All file access goes through `os.Root`. No `os.*` call takes a joined vault path. ⟨CLAUDE.md §1⟩
- **V-PATH-07** `p1` The deny list applies at every depth and ignores case. It covers the config folder, `.git/`, `.trash/` (except through the trash and restore APIs) and `node_modules/`. ⟨CLAUDE.md §1⟩
- **V-PATH-08** `p1` The config folder name is `.obsidian` unless the owner configures another name. An override name must start with `.`. ⟨configuration-folder.md §Change your configuration folder; settings.md §Override config folder⟩
- **V-PATH-09** `p1` A path matching an `OBS_MCP_DENY` glob is invisible to every operation: list, search, read and write. The default is empty. ⟨CLAUDE.md §6; DECISIONS D-001⟩
- **V-PATH-10** `p1` Writes are allowed only to `.md`, `.canvas` and `.base` files. The exception is the attachment API (V-ATT-01). Nothing under the config folder is ever writable. ⟨CLAUDE.md §1⟩
- **V-PATH-11** `p1` A regression test exists for each CVE/GHSA pattern listed in CLAUDE.md §1. ⟨CLAUDE.md §1⟩
- **V-PATH-12** `p1` Fuzz the normaliser and policy. They never panic, and every path they accept resolves inside the root. ⟨CLAUDE.md §Testing⟩

### Listing and reading
- **V-LIST-01** `p1` Listings are `iter.Seq2` streams. They skip denied paths without descending into them. ⟨CLAUDE.md §Go conventions⟩
- **V-LIST-02** `p1` Files are classified as `md`, `base`, `canvas`, image (`avif bmp gif jpeg jpg png svg webp`), audio (`flac m4a mp3 ogg wav webm 3gp`), video (`mkv mov mp4 ogv webm`), `pdf` or other. ⟨file-formats.md⟩
- **V-READ-01** `p1` Every read returns `etag` = the lowercase hex sha256 of the file's bytes. ⟨CLAUDE.md §2⟩
- **V-ERR-01** `p1` Failures are `*PathError`, `*ConflictError` (with `Current`), `*NotFoundError` or `*links.AmbiguousError`. Callers can match them with `errors.As`. ⟨CLAUDE.md §Go conventions⟩

### Writing
- **V-WRITE-01** `p1` Changing an existing file requires its `etag`. A mismatch returns `ConflictError`, carrying the current etag and a re-read hint. ⟨CLAUDE.md §2⟩
- **V-WRITE-02** `p1` Creating a file fails if it already exists (`If-None-Match: *` semantics). ⟨CLAUDE.md §2⟩
- **V-WRITE-03** `p1` Replacement is atomic: write a temp file in the same directory, fsync it, rename it over the target, then fsync the directory. The file's mode is preserved. Obsidian's reaction to the rename was not probed; etag checks make it irrelevant to correctness. ⟨CLAUDE.md §2⟩ [assumed D-006]
- **V-WRITE-04** `p1` All writes in a process are serialised. ⟨CLAUDE.md §2⟩
- **V-WRITE-05** `p1` Each file keeps its line endings (LF or CRLF) and any UTF-8 BOM through every edit. ⟨CLAUDE.md §2⟩
- **V-WRITE-06** `p1` A `.md` write with invalid UTF-8 is refused. ⟨CLAUDE.md §2⟩
- **V-WRITE-07** `p1` A dry run returns a unified diff and leaves the disk byte-identical. ⟨CLAUDE.md §2⟩
- **V-WRITE-08** `p1` A new file or folder name containing `# | ^ :`, `%%`, `[[` or `]]` is refused, because such names don't work as link targets. ⟨links.md §Invalid characters⟩ [policy]

### Trash and journal
- **V-EDIT-01** `p2` The edit ops are `replace_section`, `insert_after_heading`, `append`, `prepend`, `replace_text` (with an expected count) and `replace_block`. Each is a byte-range patch, and every byte outside the edited range is unchanged. ⟨CLAUDE.md §2 Minimal patches⟩
- **V-EDIT-02** `p2` A heading path or block ID that matches nothing, or more than one target, is refused with the candidates. A `replace_text` count mismatch is refused with the actual count. ⟨CLAUDE.md §2⟩
- **V-EDIT-03** `p2` Inserted text takes the note's line endings, and `prepend` goes after the frontmatter. ⟨CLAUDE.md §2⟩
- **V-TRASH-01** `p1` Delete moves the file to `<vault>/.trash/<same relative path>`. On a name collision a suffix is added. ⟨CLAUDE.md §2; settings.md §Deleted files⟩
- **V-TRASH-02** `p1` Restore moves a trashed file back to its original path. It fails with a clear error if that path is now occupied. ⟨CLAUDE.md §2⟩
- **V-TRASH-03** `p1` Trash entries can be listed with their original paths. ⟨CLAUDE.md §Tools⟩
- **V-JRN-01** `p1` The journal is on by default. It keeps the previous bytes of every overwrite, move and delete in `~/.local/state/obs-mcp/journal`, outside the vault. `OBS_MCP_JOURNAL` can turn it off or set the path. ⟨CLAUDE.md §2; DECISIONS D-001⟩
- **V-JRN-02** `p1` The journal is capped at 500 MB by default, and the oldest entries are pruned first. ⟨DECISIONS D-001⟩
- **V-JRN-03** `p5` Undo restores the state from before the last journaled write. Its dry run shows the diff. Undo itself goes through the etag check. ⟨CLAUDE.md §Tools⟩

### Vault configuration
- **V-CFG-01** `p1` These `app.json` keys are read: `newLinkFormat`, `useMarkdownLinks`, `alwaysUpdateLinks`, `newFileLocation`, `newFileFolderPath`, `attachmentFolderPath`, `userIgnoreFilters`. Obsidian writes only keys changed from the default. Missing keys take the defaults observed in D-006: `shortest`, `false`, `false`, `root`, `/`, `/`, none. ⟨settings.md §Files and links⟩ [probed D-006]
- **V-CFG-02** `p1` These files are read when present: `daily-notes.json` (folder, format, template), `templates.json` (folder, dateFormat, timeFormat) and `core-plugins.json` (an object mapping plugin id to bool). Unknown keys are ignored, and a missing file or key means the documented default. ⟨daily-notes.md; templates.md⟩ [assumed D-006]
- **V-CFG-03** `p1` A missing `.obsidian/` folder, or an `app.json` of `{}`, is valid and gives all-default settings. ⟨CLAUDE.md §Facts⟩ [probed D-006]
- **V-LOCK-01** `p1` Obsidian keeps no lock file in the vault while it is open, so none is checked or created. ⟨CLAUDE.md §Known undocumented⟩ [probed D-006]

### Search
- **V-SRCH-01** `p3` Full-text BM25 search over `.md` files returns ranked paths with snippets and 1-based line numbers. It is paginated. ⟨CLAUDE.md §Tools⟩
- **V-SRCH-02** `p3` Search filters: tag, property=value, path glob, and modified-since. Filters combine with AND. ⟨CLAUDE.md §Tools⟩
- **V-SRCH-03** `p3` Paths matching `userIgnoreFilters` (Excluded files; a folder entry is stored with a trailing `/`) are left out of search, tag listings, orphans and unresolved-link listings. They stay in the link index, so their links still count as backlinks, and they can still be read directly. ⟨settings.md §Excluded files⟩ [probed D-006]
- **V-SRCH-04** `p3` A tag filter ignores case and matches nested tags: `inbox` matches `inbox/to-read`. ⟨tags.md §Nested tags⟩
- **V-SRCH-05** `p3` `list_tags` returns case-folded tags with counts and nesting. Each tag is displayed with the casing of its first occurrence in vault path order. ⟨tags.md §Tag format⟩ [policy]
- **V-SRCH-06** `p3` The index builds lazily and updates incrementally by mtime. A 10k-note fixture vault indexes inside the call timeout. ⟨CLAUDE.md §3⟩

### Daily notes, templates and attachments
- **V-DAILY-01** `p5` The daily-note path is `<folder>/<date formatted with format>.md`, with default format `YYYY-MM-DD`. A `/` produced by the format creates subfolders. Unsupported moment tokens are refused, not guessed. ⟨daily-notes.md⟩ [assumed D-006]
- **V-DAILY-02** `p5` Opening a missing daily note creates it from the configured template, with variables expanded. It works for any date. ⟨daily-notes.md §Create a daily note from template⟩
- **V-TPL-01** `p5` Template variables: `{{title}}`, `{{date}}` (default `YYYY-MM-DD`), `{{time}}` (default `HH:mm`), plus `{{date:FMT}}` and `{{time:FMT}}` with moment tokens. The defaults can be overridden in `templates.json`. Any other `{{…}}` and all `<% %>` text is copied verbatim. ⟨templates.md §Template variables⟩ [assumed D-006]
- **V-TPL-02** `p5` Any other `{{…}}` text and all Templater `<% … %>` code are copied verbatim and never executed. ⟨CLAUDE.md §5⟩
- **V-ATT-01** `p5` `attach_file` copies a local file into the folder chosen by `attachmentFolderPath`: `/` or missing = vault root, `./` = same folder as the note, `./<name>` = subfolder `<name>` of the note's folder, anything else = that vault folder. It returns embed text in the vault's link style. ⟨settings.md §Default location for new attachments⟩ [assumed D-006]

## O: OFM scanner

- **O-SPAN-01** `p2` The scanner returns typed byte spans for frontmatter, headings, sections, blocks, block IDs, links, embeds, tags, callouts, comments, code, math and tasks. Spans either nest or don't overlap. Fuzzing never panics. ⟨CLAUDE.md §Layout, §Testing⟩
- **O-FM-01** `p2` Frontmatter starts with a line that is exactly `---` at byte 0 (after an optional BOM; LF or CRLF) and ends at the next line that is exactly `---`. A trailing space, a leading blank line, `----`, an indented fence, a `...` close or a missing close all make it body text. An empty or invalid block is still frontmatter, with no properties. ⟨properties.md §Property format⟩ [probed D-006]
- **O-FM-02** `p2` JSON frontmatter (`---` followed by a `{…}` object) is recognised and read. ⟨properties.md §JSON properties⟩
- **O-HEAD-01** `p2` ATX headings use 1–6 `#` characters followed by a space, indented at most 3 spaces; closing `#`s are dropped from the text. Setext headings (`===` → 1, `---` → 2) are headings too. A heading section runs to the next heading of the same or a higher level. ⟨syntax.md §Headings⟩ [probed D-006]
- **O-CODE-01** `p2` Fenced code uses ≥3 backticks or tildes, and closes with the same character at ≥ the opening length. Indented code is 4 spaces or a tab. Its content is opaque: no links, tags or block IDs are recognised inside. ⟨syntax.md §Code blocks⟩
- **O-CODE-02** `p2` Inline code spans, including multi-backtick spans, are opaque. ⟨syntax.md §Inline code⟩
- **O-CMT-01** `p2` An inline `%%…%%` comment stays on one line. A block comment starts with a line beginning `%%` and ends at the next `%%`. An unclosed block comment runs to the end of the file. Obsidian indexes links inside comments, so they count as links for backlinks and renames. ⟨syntax.md §Comments⟩ [probed D-006]
- **O-MATH-01** `p2` `$…$` and `$$…$$` math spans are opaque. ⟨advanced-syntax.md §Math⟩ [policy]
- **O-LINK-01** `p2` Wikilinks: `[[target]]`, `[[target.md]]`, `[[target#sub]]`, `[[target|display]]` and `[[#sub]]`. In a table, `\|` is the display separator. ⟨links.md; advanced-syntax.md §Format content within a table⟩
- **O-LINK-02** `p2` Markdown internal links: `[text](dest)`, where `dest` is percent-encoded or wrapped in `<…>`, and `.md` is optional. A destination with an unencoded space and no `<…>` is not a link. A destination with a URI scheme is an external link. ⟨links.md §Supported formats; syntax.md §Escape blank spaces in links⟩ [probed D-006]
- **O-LINK-03** `p2` Subpaths: `#Heading`, `#H#Sub` (a chain of headings), and `#^blockid`. ⟨links.md §Link to a heading, §Link to a block⟩
- **O-LINK-04** `p2` Embeds are `!` plus either link form. A trailing `|W` or `|WxH` on an image embed is a size, not display text. PDF subpaths `#page=N`, `#height=N` and their `&` combination are kept opaque. ⟨embeds.md; CHANGELOG-excerpts 1.12.0⟩
- **O-LINK-05** `p2` A link to a non-Markdown file includes its extension. ⟨links.md §Link to a file⟩
- **O-LINK-06** `p2` Quoted wikilinks in frontmatter text or list values are links. ⟨properties.md §Text, §List; bases-syntax.md §Files and links⟩
- **O-BLK-01** `p2` Block IDs match `^[A-Za-z0-9-]+`. A block ID sits at the end of a paragraph or list-item line after a space, or alone on a line after a structured block (list, quote, callout or table), separated by blank lines. ⟨links.md §Link to a block in a note⟩
- **O-TAG-01** `p2` An inline tag is `#` followed by letters, digits, `_`, `-`, `/` or other Unicode such as emoji, and it ends at any other character. It needs at least one non-digit, ignores case, and nests with `/`. A `#` directly after a letter, digit or `(` does not start a tag; `#a#b` is two tags; URL fragments and code are not tags. ⟨tags.md §Tag format⟩ [probed D-006]
- **O-TAG-02** `p2` Frontmatter `tags` as a list (block or flow) gives one tag per item, with a leading `#` stripped. A plain one-word scalar is one tag. A scalar containing a space or comma gives no tags. The singular `tag` key gives no tags. ⟨tags.md; properties.md §Tags⟩ [probed D-006]
- **O-CALL-01** `p2` A callout is `> [!type]` with an optional `+` or `-` and a title. Callouts can be nested. Types ignore case. The built-in types are `note abstract info todo tip success question warning failure danger bug example quote`, with aliases `summary tldr hint important check done help faq caution attention fail missing error cite`. Unknown types are treated as `note`. ⟨callouts.md⟩
- **O-TASK-01** `p2` A task is a list item starting `[c]`, where `c` is any single character. A space means incomplete. ⟨syntax.md §Task lists⟩
- **O-INL-01** `p2` Highlights `==…==` (including colour-emoji variants), footnotes `[^id]`, and inline footnotes `^[…]` are recognised, so a `^` inside them is never taken for a block ID. ⟨syntax.md §Bold, italics, highlights, §Footnotes⟩
- **O-HTML-01** `p2` Links inside inline HTML (`<span>…</span>`) are indexed. Links inside an HTML block (a line starting with a block tag such as `<div>`) are not. ⟨obsidian-flavored-markdown.md⟩ [probed D-006]

## P: Properties

- **P-FM-01** `p2` Setting, removing or renaming a key rewrites only that key's lines. All other bytes stay identical, including key order, comments, quoting and indentation. ⟨CLAUDE.md §4⟩
- **P-FM-02** `p2` Patching then reparsing gives the requested value, with every other key unchanged. This is verified by fuzzing. ⟨CLAUDE.md §Testing⟩
- **P-TYPE-01** `p2` Types come from `.obsidian/types.json`, shaped `{"types": {name: type}}` with type one of `text`, `multitext`, `number`, `checkbox`, `date`, `datetime`, `tags`, `aliases`. A type applies to that property name vault-wide. Changing a type rewrites no notes. ⟨properties.md §Property types⟩ [probed D-006]
- **P-TYPE-02** `p2` Number: an integer or decimal literal, never an expression. ⟨properties.md §Number⟩
- **P-TYPE-03** `p2` Checkbox: `true` or `false`. An empty value is indeterminate and is preserved. ⟨properties.md §Checkbox⟩
- **P-TYPE-04** `p2` Date: `YYYY-MM-DD`. Date & time: `YYYY-MM-DDTHH:mm:ss`, and `YYYY-MM-DDTHH:mm` is read too. New values are written unquoted with seconds, as Obsidian's picker writes them. Both are validated as real calendar values. ⟨properties.md §Date, §Date & time⟩ [probed D-006]
- **P-TYPE-05** `p2` The Tags type is used only by `tags`, and assigning it to another name is refused. ⟨properties.md §Tags⟩
- **P-TYPE-06** `p2` A value whose name has no recorded type is accepted. Its inferred type follows the table in D-006 (for example, a number is `number`, `2024-02-29` is `date`, a quoted number is `text`). ⟨properties.md⟩ [probed D-006]
- **P-TYPE-07** `p2` A value that conflicts with the recorded type is refused, with a hint naming the type. ⟨CLAUDE.md §4⟩
- **P-LINK-01** `p2` Internal links written into text or list values are always quoted. ⟨properties.md §Text, §List⟩
- **P-KEY-01** `p2` Duplicate keys in a note are reported. A write never creates a duplicate. ⟨properties.md §Property format⟩
- **P-KEY-02** `p2` `tag`, `alias` and `cssclass` are read as `tags`, `aliases` and `cssclasses`. Writes use only the plural forms. ⟨properties.md §Deprecated properties⟩
- **P-KEY-03** `p2` A write never creates a nested (map) value. An existing nested value is preserved byte for byte. ⟨properties.md §Not supported⟩
- **P-JSON-01** `p2` JSON frontmatter is read-only. A property write to it is refused with a hint, because a minimal patch isn't possible and Obsidian would re-save it as YAML. ⟨properties.md §JSON properties; CLAUDE.md §2⟩ [policy]
- **P-DATE-01** `p2` Date values are never reformatted. They keep their written form. ⟨CLAUDE.md §Go conventions⟩
- **P-REN-01** `p5` `rename_property` renames a key vault-wide. Its dry run lists every file. A file that already has the target key is reported and skipped. ⟨CLAUDE.md §Tools⟩
- **P-TAGREN-01** `p5` `rename_tag` renames a tag in inline tags and in frontmatter `tags`, ignoring case. Nested children follow the rename (`a` → `b` renames `a/x` to `b/x`). Code and comments are left alone. ⟨tags.md⟩

## L: Links

- **L-IDX-01** `p3` The index holds every link and embed in `.md` files (body and frontmatter), every `.canvas` file node, and every `.base` embed. ⟨CLAUDE.md §3; CHANGELOG-excerpts 1.12⟩
- **L-RES-01** `p3` A target resolves with or without `.md`. A target containing `/` matches a file whose path ends with it (`a/Tie` finds `pr01/a/Tie.md`), or the full vault path. ⟨links.md §Supported formats⟩ [probed D-006]
- **L-RES-02** `p3` A bare name shared by several files resolves to the one in the source note's folder, then to the one with the shortest path. If several remain, the result is `AmbiguousError` with the candidates (Obsidian's own choice there is not predictable). ⟨CLAUDE.md §3⟩ [probed D-006]
- **L-RES-03** `p3` Target matching ignores case. ⟨CLAUDE.md §Known undocumented⟩ [probed D-006]
- **L-RES-04** `p3` Frontmatter `aliases` do not take part in resolution: `[[Alias]]` is unresolved unless a file has that name. ⟨links.md §Change the link display text⟩ [probed D-006]
- **L-RES-05** `p3` Markdown-link destinations are percent-decoded or `<…>`-unwrapped, then resolved like wikilinks; `./`, `../` and a leading `/` work, and a bare file name falls back to name lookup. ⟨links.md⟩ [probed D-006]
- **L-RES-06** `p3` `resolve_link` returns the resolved path, or every candidate for an ambiguous link. ⟨CLAUDE.md §Tools⟩
- **L-BACK-01** `p3` Backlinks include links from `.md` files and from `.canvas` file nodes. ⟨CHANGELOG-excerpts 1.12⟩
- **L-UNRES-01** `p3` Unresolved links are listed with their source file and line. ⟨cli.md §unresolved⟩
- **L-ORPH-01** `p3` An orphan is a file with no incoming links. ⟨cli.md §orphans⟩
- **L-REN-01** `p5` A note rename or move rewrites every link that resolves to the old path: wikilinks, Markdown links, embeds, frontmatter links and canvas file nodes. Subpaths and display text are kept. ⟨CLAUDE.md §3; links.md⟩
- **L-REN-02** `p5` A rewritten link uses the vault's `newLinkFormat`: shortest = the bare name when no other file has it, else the full vault path; relative = the path from the note's folder; absolute = the full vault path. `.md` is dropped and other extensions kept. Markdown style writes `[name](dest)` with only spaces percent-encoded. ⟨settings.md §New link format, §Use Wikilinks⟩ [probed D-006]
- **L-REN-03** `p5` Links inside code and math are never rewritten. Links inside comments are links to Obsidian, so they are rewritten (D-006, D-012). ⟨CLAUDE.md §3⟩ [probed D-006]
- **L-REN-04** `p5` A link that would become ambiguous is reported and left unchanged. ⟨CLAUDE.md §3⟩
- **L-REN-05** `p5` A rename dry run lists every file and line that would change, with the diff. ⟨CLAUDE.md §3⟩
- **L-REN-06** `p5` A rename is all-or-nothing. If any write fails, every file already written is restored. ⟨CLAUDE.md §3⟩
- **L-REN-07** `p5` A rename round trip A → B → A restores every file byte for byte. ⟨CLAUDE.md §Testing⟩
- **L-REN-08** `p5` `rename_heading` updates `[[Note#Heading]]` links, including `#H#Sub` chains. The link text is the heading text as written, with closing `#`s dropped. ⟨links.md §Link to a heading⟩ [probed D-006]
- **L-REN-09** `p5` `rename_block` renames a `^id` and updates `[[Note#^id]]` links (D-012). ⟨links.md §Link to a block⟩
- **L-REN-10** `p5` `move_folder` is link-aware, with the same guarantees as L-REN-01 to L-REN-07. ⟨CLAUDE.md §Tools⟩

## C: Canvas (JSON Canvas 1.0)

- **C-SCH-01** `p6` The top level has optional `nodes` and `edges` arrays. ⟨spec-1.0.md §Top level⟩
- **C-NODE-01** `p6` Every node has `id`, `type` (`text`, `file`, `link` or `group`), and integer `x`, `y`, `width` and `height`. `color` is optional. ⟨spec-1.0.md §Generic node⟩
- **C-NODE-02** `p6` Node and edge IDs are unique. Edits generate IDs that are unique across both. ⟨spec-1.0.md §Generic node, §Edges⟩ [policy for cross-kind uniqueness]
- **C-NODE-03** `p6` Type fields: `text` has `text`; `file` has `file` and an optional `subpath` starting with `#`; `link` has `url`; `group` has optional `label` and `background`, and optional `backgroundStyle` of `cover`, `ratio` or `repeat`. ⟨spec-1.0.md §Text/File/Link/Group⟩
- **C-EDGE-01** `p6` An edge has `id`, `fromNode` and `toNode`, which must reference existing nodes. Optional fields: `fromSide` and `toSide` (`top`, `right`, `bottom` or `left`), `fromEnd` (default `none`) and `toEnd` (default `arrow`), each `none` or `arrow`, plus `color` and `label`. ⟨spec-1.0.md §Edges⟩
- **C-COL-01** `p6` A colour is a hex string or a preset from `"1"` to `"6"`. ⟨spec-1.0.md §Color⟩
- **C-ORD-01** `p6` Node array order is z-order and is preserved by edits. New nodes are appended. ⟨spec-1.0.md §Nodes⟩
- **C-PRES-01** `p6` Unknown keys and key order are preserved. Edits keep the file's existing indentation; new files use tab indentation. ⟨CLAUDE.md §2⟩ [policy]
- **C-EDIT-01** `p6` `edit_canvas` adds, updates and removes nodes and edges. Removing a node also removes its edges. The result is validated against C-SCH-01 to C-COL-01. An etag is required. ⟨CLAUDE.md §Tools⟩

## B: Bases

- **B-SCH-01** `p6` A `.base` file is YAML with the top-level keys `filters`, `formulas`, `properties`, `summaries` and `views`. Unknown keys are preserved. ⟨bases-syntax.md⟩
- **B-FLT-01** `p6` A filter is a statement string, or an object with exactly one of `and`, `or` or `not` whose value is a list of filters. The definition is recursive. ⟨bases-syntax.md §Filters⟩
- **B-FRM-01** `p6` `formulas` maps names to expression strings. ⟨bases-syntax.md §Formulas⟩
- **B-PROP-01** `p6` `properties` maps property IDs (`name`, `note.x`, `file.x` or `formula.x`) to config objects such as `displayName`. ⟨bases-syntax.md §Properties⟩
- **B-SUM-01** `p6` The top-level `summaries` maps names to formula strings. A view's `summaries` maps property IDs to a default summary name or a custom one. ⟨bases-syntax.md §Summaries⟩
- **B-VIEW-01** `p6` `views` is a list. Each view has `type` (any string) and `name`, plus optional `limit` (an integer), `order` (a list of property IDs), `groupBy` (`property`, and `direction` of `ASC` or `DESC`), `groupOrder` (a list, which may contain `null`), `filters` and `summaries`. Extra keys are preserved. ⟨bases-syntax.md §Views; CHANGELOG-excerpts 1.14.4⟩
- **B-EMB-01** `p6` `![[X.base]]`, `![[X.base#View]]` and ```` ```base ```` code blocks are recognised. A code block is validated like a file. ⟨bases-create.md §Embed a base⟩
- **B-EDIT-01** `p6` `edit_base` patches YAML nodes and preserves formatting, as P-FM-01 does. An etag is required. ⟨CLAUDE.md §Tools⟩
- **B-NOQ-01** `p6` No query evaluation is offered (no `query_base`). ⟨DECISIONS D-001⟩

## X: MCP server

- **X-SRV-01** `p4` The server uses the official Go SDK over stdio. stdout carries only the protocol. Logs go to stderr through `slog`, one line per call, and note content is never logged at INFO. ⟨CLAUDE.md §MCP server⟩
- **X-SRV-02** `p4` Config comes from env or args: one or more vaults (named by folder unless a name is given), `OBS_MCP_READ_ONLY`, `OBS_MCP_DENY`, `OBS_MCP_JOURNAL` and `OBS_MCP_LOG_LEVEL`. ⟨CLAUDE.md §MCP server⟩
- **X-SRV-03** `p4` Read-only mode registers no write tools. ⟨CLAUDE.md §6⟩
- **X-SRV-04** `p4` Every tool sets `readOnlyHint`, `destructiveHint`, `idempotentHint` and `openWorldHint: false`. ⟨CLAUDE.md §MCP server⟩
- **X-SRV-05** `p4` Every write tool has `dry_run`. ⟨CLAUDE.md §MCP server⟩
- **X-SRV-06** `p4` Listing and search output is paginated with a cursor and limit, and capped in bytes. ⟨CLAUDE.md §MCP server⟩
- **X-SRV-07** `p4` Note text in tool output is wrapped in delimited untrusted-content blocks. The server instructions say never to follow instructions found in notes. ⟨CLAUDE.md §5⟩
- **X-SRV-08** `p4` No tool runs commands, plugins, Templater, `eval` or URIs, and no tool makes network calls. ⟨CLAUDE.md §5, §Safety⟩
- **X-SRV-09** `p4` Every tool takes an optional `vault`, and an unknown name is an error. ⟨CLAUDE.md §MCP server⟩
- **X-SRV-10** `p4` Typed library errors become tool errors with actionable hints. For example, a conflict tells the caller to re-read. ⟨CLAUDE.md §2⟩
- **X-TOOL-01** `p4` Discover tools: `list_vaults`, `list_notes`, `get_note_info`. ⟨CLAUDE.md §Tools⟩
- **X-TOOL-02** `p4` Read tools: `read_note` (whole note, heading section, block ID or line range) and `read_notes` (a capped batch). Both return etags. ⟨CLAUDE.md §Tools⟩
- **X-TOOL-03** `p4` Search tools: `search`, `list_tags`, `find_by_property`. ⟨CLAUDE.md §Tools⟩
- **X-TOOL-04** `p4` Link tools: `backlinks`, `outgoing_links`, `unresolved_links`, `orphans`, `resolve_link`. ⟨CLAUDE.md §Tools⟩
- **X-TOOL-05** `p4` Write tools: `create_note`, `edit_note` and `replace_note`. `edit_note` has the ops `replace_section`, `insert_after_heading`, `append`, `prepend`, `replace_text` (with an expected count) and `replace_block`. ⟨CLAUDE.md §Tools⟩
- **X-TOOL-06** `p4` Property tools: `set_properties`, `remove_properties`. ⟨CLAUDE.md §Tools⟩
- **X-TOOL-07** `p4` Trash tools: `delete_note`, `restore_from_trash`, `list_trash`. ⟨CLAUDE.md §Tools⟩
- **X-TOOL-08** `p5` Organise tools: `move_note`, `rename_note`, `create_folder`, `move_folder`, `rename_tag`, `rename_property`, `rename_heading`, `rename_block`. ⟨CLAUDE.md §Tools; D-012⟩
- **X-TOOL-09** `p5` Daily-note and template tools: `open_daily_note`, `append_to_daily_note`, `list_templates`, `create_from_template`. ⟨CLAUDE.md §Tools⟩
- **X-TOOL-10** `p5` `attach_file` and `undo_last_write`. ⟨CLAUDE.md §Tools⟩
- **X-TOOL-11** `p6` Canvas and Bases tools: `read_canvas`, `edit_canvas`, `read_base`, `create_base`, `edit_base`. ⟨CLAUDE.md §Tools⟩

## X-PLUG: Packaging

- **X-PLUG-01** `p7` The `.mcpb` manifest sets `manifest_version` `"0.3"`, `server.type` `binary`, `entry_point` `server/obs-mcp`, `mcp_config.command` `${__dirname}/server/obs-mcp`, and `compatibility.platforms` `["darwin"]`. ⟨CLAUDE.md §Packaging⟩
- **X-PLUG-02** `p7` `user_config` defines `vaults` (`directory`, `multiple`, required), `read_only` (default false), `deny` (default `""`) and `journal` (default true). Every optional setting has a default. ⟨CLAUDE.md §Packaging⟩
- **X-PLUG-03** `p7` A Go re-implementation of Desktop's `getMcpConfigForManifest` substitution produces the expected argv and env. Booleans become strings, and the vault array expands only from an exact `"${user_config.vaults}"` arg. ⟨CLAUDE.md §Packaging⟩
- **X-PLUG-04** `p7` The manifest sets only settings the server reads. ⟨CLAUDE.md §Packaging⟩
- **X-PLUG-05** `p7` The plugin version equals the manifest version. ⟨CLAUDE.md §Packaging⟩
- **X-PLUG-06** `p7` Every tool a skill names exists in the server. ⟨CLAUDE.md §Packaging⟩
- **X-PLUG-07** `p7` The plugin has no top-level `bin/`. ⟨CLAUDE.md §Packaging⟩
- **X-PLUG-08** `p7` Each skill is ≤200 lines. Frontmatter `name` equals the folder name (lowercase with hyphens, ≤64 characters), and `description` is ≤1024 characters. ⟨CLAUDE.md §Packaging⟩
- **X-PLUG-09** `p7` Only `dist/obs-mcp-darwin-arm64.mcpb` is built. `verify-static` unzips it and checks the binary inside. ⟨CLAUDE.md §Packaging, §Build⟩

---

## Probe results

The probe results and the assumptions made where probing stopped are in `docs/DECISIONS.md` (D-006).
