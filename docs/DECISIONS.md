# Decisions

Non-obvious choices and probe results, newest last. Each entry gives the date and the reason.

## D-001 Owner decisions (Phase 0, 2026-10-25)

| Topic | Decision |
|---|---|
| Repo and module | Repo `obsidian-mcp`, module `github.com/swiftj/obsidian-mcp`. The binary stays `obs-mcp`. |
| Vault location | Plain local folders. There is no sync layer, so sync-conflict handling is low priority: it is documented, but not built beyond etag checks. |
| Journal | On by default, at `~/.local/state/obs-mcp/journal`, capped at 500 MB. The oldest entries are pruned first. |
| Privacy | `OBS_MCP_DENY` ships empty by default. The feature itself is still built. |
| Phase 6 | No Obsidian CLI backend and no `query_base`. Bases are parsed, validated and edited only. `internal/obscli/` is not created. |
| Probes | Manual. The CLI is not enabled on the owner's machine. Claude builds scratch vaults and a checklist, the owner acts in Obsidian, and Claude diffs what Obsidian wrote. |

## D-002 Obsidian version under test (2026-10-25)

The installed app is **1.14.4**: the asar is `obsidian-1.14.4.asar`, and the installer is 1.6.7. The latest release is 1.15.0 (2026-10-22). Probe results are recorded against 1.14.4. Before relying on a probe for behaviour the 1.15 changelog mentions, re-check it on 1.15.

## D-003 Tooling (2026-10-25)

- Go 1.27.3. There are no dependencies yet.
- The Makefile sets a private `GOCACHE` (`.gocache/`, which is gitignored).
- golangci-lint 2.15 with a small linter set (`.golangci.yml`). misspell uses its default locale, because spec field names such as JSON Canvas `color` are US spellings.

## D-004 Source pinning and release status (2026-10-25)

- **Help pages:** pinned to `obsidianmd/obsidian-help@f333c95` and the JSON Canvas spec to `obsidianmd/jsoncanvas@456f843` (see `docs/obsidian/FETCHED.md`). The help pages are byte-identical to what obsidian.md/help serves today.
- **Correction to CLAUDE.md:** 1.15.0 (2026-10-22) is an **Early access** release. The latest **public** release is **1.14.5**, published the same day. Its notes list no changes in our areas. Requirements are written against public behaviour. Anything that exists only in 1.15 EA is tagged as such in the spec.

## D-005 Spec contract conventions (2026-10-25)

- **IDs and phases:** IDs are `<GROUP>-<AREA>-<NN>`, and each carries a phase tag `pN`. `internal/speccov` enforces coverage for every ID with phase ≤ `docs/PHASE`. A test "names" an ID by containing it anywhere in a `_test.go` file. `testdata/` is excluded from that scan.
- **New groups:** `M-*` (meta: spec integrity and source checksums) was added beyond the groups in CLAUDE.md. Search, daily notes, templates and attachments sit under `V-*`, because they live in `vault/` subpackages.
- **Policy items:** `[policy]` marks our own choices where Obsidian defines nothing. These need owner review:
  - V-WRITE-08: refuse new names with link-breaking characters
  - V-SRCH-05: tag display casing
  - O-MATH-01: math spans are opaque
  - P-JSON-01: JSON frontmatter is read-only
  - C-NODE-02: generated IDs are unique across nodes and edges

## D-006 Probe results, Obsidian 1.14.4 (2026-10-25)

Method: a made-up scratch vault (`testdata/probes/vault/`) opened in Obsidian 1.14.4 on macOS. The evidence is what Obsidian wrote to disk, plus a dump of its own metadata cache, resolver and link generator (`scripts/probe-dump.js`). The owner stopped the manual probing after the settings step. Items that weren't probed are marked **assumed**: they follow the docs and existing servers, and use the conservative choice.

**Vault files (probed)**
- Opening a vault creates `.obsidian/` with `app.json` `{}`, `appearance.json` `{}`, `core-plugins.json` and `workspace.json`. JSON is 2-space indented, with no trailing newline.
- `core-plugins.json` is an object mapping plugin id to bool. Daily notes, Templates, Bases, Canvas and Properties are on by default.
- No lock file exists while the vault is open (V-LOCK-01).
- `app.json` holds only keys changed from the default. The defaults are `newLinkFormat` `shortest`, `useMarkdownLinks` false, `alwaysUpdateLinks` false, `newFileLocation` `root`, `newFileFolderPath` `/`, `attachmentFolderPath` `/`, and no `userIgnoreFilters`.
- Observed values: `"newFileLocation": "folder"` with `"newFileFolderPath": "pr06/excluded"`, and `"userIgnoreFilters": ["pr06/excluded/"]`. A folder picked in Manage is stored with a trailing `/`.
- Excluded files are hidden from views, but their links stay in the link index.
- `types.json` is `{"types": {name: type}}`, written the first time a type changes, and it also records `aliases`, `cssclasses` and `tags`. Types seen: `text`, `multitext`, `number`, `checkbox`, `date`, `datetime`. Changing a type rewrote no note.
- The date-time picker wrote `p_picked: 1972-05-13T00:00:00`: unquoted, with seconds, and appended as the last key. All other bytes were unchanged.

**Link resolution (probed)** (L-RES-01..05)
- Case-insensitive.
- `.md` is optional.
- A path-bearing target matches by path suffix (`a/Tie` finds `pr01/a/Tie.md`).
- Aliases do **not** resolve.
- Same-named files resolve as follows:
  - the file in the source's folder wins
  - otherwise the shortest path wins
  - between equal-length paths (`pr01/a/Tie`, `pr01/b/Tie`) Obsidian chose `b`, which is not alphabetical, so we report these as ambiguous
- Markdown destinations are percent-decoded, and `./`, `../` and a leading `/` work. A destination with an unencoded space and no `<…>` is not a link.

**Link generation (probed)** (L-REN-02)

| Format | Link written |
|---|---|
| shortest | the bare name if it is unique in the vault, otherwise the full path |
| relative | the path from the note's folder (`../a/Tie`) |
| absolute | the full vault path |

- `.md` is dropped, and other extensions are kept.
- Markdown style is `[Name](dest.md#sub)` with only spaces encoded as `%20`.

**Where links count (probed)** (O-CMT-01, O-HTML-01)
- **Indexed:** normal text, `%%inline%%` and `%%` block comments, inline HTML, callouts, tables (`\|` alias), embeds, Markdown links, and quoted frontmatter values (including list items).
- **Not indexed:** fenced, inline and indented code; math; HTML blocks; escaped `\[[`; unquoted `[[x]]` in frontmatter.
- **Deviation from CLAUDE.md §3:** CLAUDE.md said to skip links in comments when renaming. Because Obsidian counts them as links, we index and rewrite them, so a rename never leaves a stale backlink.

**Markdown structure (probed)** (O-FM-01, O-HEAD-01, O-TAG-*)

*Frontmatter*
- **Recognised:** an exact `---` opening and closing line, with LF or CRLF, a BOM (offsets exclude it), or a JSON body.
- **Not recognised:** `--- `, a leading blank line, `----`, indented fences, a `...` close, or no close.
- **Empty or invalid YAML:** the block is still frontmatter, with no properties.

*Headings*
- ATX headings with at most 3 spaces of indent, plus setext headings.
- Closing `#`s are dropped.
- `#NoSpace` is a tag, not a heading.
- Heading text keeps `[[..|..]]`, `**`, `%%`, `#`, `|` and `^` as written.

*Block IDs*
- Recognised: ` ^id` at the end of a paragraph or list item, or alone on the line after a quote or table.
- Not recognised: `^under_score`, `x^id` with no space, and IDs inside code.

*Tags*
- **Tags:** `#y1984`, `#émoji🙂`, `#日本語`, `#-lead`, `#_lead`, `#/slash` and `**#bold**`.
- **Split or trimmed:** `#a#b` gives two tags, `#trail.` gives `trail`, and `#tag,comma` gives `tag`.
- **Not tags:** `#123`, `word#x`, `(#paren)`, URL fragments, and code.
- **Case and nesting:** case is merged, and parent tags are listed.
- **Frontmatter `tags`:**
  - a list (block or flow) gives one tag per item, with `#` stripped
  - a one-word scalar gives one tag
  - `one, two` and `two words` give none
  - singular `tag:` gives none

**Type inference with no `types.json` entry (probed)** (P-TYPE-06)

| Value | Inferred |
|---|---|
| `7`, `3.14` | number |
| `false` | checkbox |
| `2024-02-29`, `"2024-01-15"` | date |
| `2024-02-29T08:15` | datetime |
| `2024-02-29T08:15:00` | **text** |
| `"42"`, `"[[README]]"`, empty, `null` | text |
| block or flow list | multitext |
| nested map, unquoted `[[x]]` | unknown |

**Assumed, not probed**
- **Watcher (PR-08):** we write atomically (temp file and rename). Lost updates are prevented by etags, not by Obsidian's watcher.
- **Daily notes and templates (PR-11):** both option files were `{}` at their defaults. We read the documented keys (`folder`, `format`, `template`; `folder`, `dateFormat`, `timeFormat`), ignore unknown keys, and use the documented defaults.
- **Attachment location (PR-06):**
  - `/` = vault root, `./` = same folder, `./<name>` = subfolder, anything else = that folder
  - this is the encoding existing Obsidian tools use; only the default `/` was observed
- **Renames (PR-01/04/13/14 rewrite side):** we rewrite every link the index resolves to the old path, in the configured format.
- **Alias click (PR-03):** irrelevant, because aliases don't resolve.
- **Canvas formatting (PR-16):** [policy] keep the file's existing indentation; new files use tabs.
