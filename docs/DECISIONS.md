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

## D-007 YAML library: `go.yaml.in/yaml/v3` (2026-10-25)

Spiked in a scratch module against `go.yaml.in/yaml/v3` v3.0.5 and `github.com/goccy/go-yaml` v1.19.2. Both build with `CGO_ENABLED=0`.

| Check | yaml/v3 | goccy |
|---|---|---|
| Position per key and value | line and column (columns count runes) | byte offset, line and column (byte offsets correct with UTF-8 and CRLF) |
| End position of a value | no | no |
| Comments | head and line comments on nodes | attached to nodes with `ParseComments` |
| `yes` | string (YAML 1.2) | string |
| `1e3` | float (YAML 1.2 core, as JS parsers read it) | **string** |
| `2024-01-15` | `!!timestamp` tag, raw text kept | string |

**Choice: `go.yaml.in/yaml/v3`.**
- **Edits don't need parser byte offsets.** Edits are line-span patches: a key's span runs from its line to the line before the next sibling key. Neither library gives end positions, so we compute spans from our own line table. Line and column are enough for that.
- **Scalar typing matches YAML 1.2 core.** That is what Obsidian's JavaScript parser uses, and it matters for type inference and validation.
- **It is the maintained continuation of `gopkg.in/yaml.v3`,** under the YAML organisation.

The dependency is added in Phase 2, not before.

## D-008 Vault core choices (Phase 1, 2026-10-25)

- **Trash naming [policy]:**
  - Deleted files go to `.trash/<same path>`.
  - On a collision the name becomes `<stem>~N<ext>`, with the lowest free N. `ListTrash` strips a trailing `~N` to recover the original path.
  - A file whose real name ends in `~N` therefore restores to the stripped name by default. `Restore` takes an explicit destination for that case.
  - Moves into and out of `.trash` link and then unlink, so they never overwrite an existing file.
- **Creates never overwrite:** the content is written to a temp file and hard-linked into place. If the filesystem has no hard links, an exclusive create is used instead.
- **Replace checks the etag twice:** once before writing, and again just before the rename. That narrows the window for an Obsidian save landing in between.
- **Journal layout:**
  - Entries live in `<journal>/<vault name>-<hash of the vault path>/<time>-<seq>/`, holding `meta.json` plus `data` (the previous bytes). Files are `0600`.
  - Create and restore entries keep no data.
  - Pruning removes the oldest entries across all vaults until the total is within the cap, always keeping the newest.
  - If the journal is on and recording fails, the write is refused.
- **Listing:**
  - Names starting with `.`, symlinks, and hidden or denied paths are skipped without descending into them.
  - Each folder's entries are sorted by name.
  - `webm` is classified as video; file-formats.md lists it under both audio and video.
- **Hardening beyond the spec:** paths with control characters or invalid UTF-8 are refused, as are `.md` writes with invalid UTF-8.
- **Plan deviation:** steps 2–4 of the Phase 1 plan were committed together, because their tests share fixtures and call across trash, write and journal.

## D-009 Scanner, properties and edit choices (Phase 2, 2026-10-25)

**Dependency:** `go.yaml.in/yaml/v3` v3.0.5, added for frontmatter (D-007). The binaries stay CGO-free, which `make verify-static` checks.

**Scanner (`vault/ofm`)**
- **Inline scanning is per line.** Inline code, math and Markdown links don't span lines. Wikilinks and inline `%%` comments can't in Obsidian either.
- **Comments:** links inside `%%` comments are reported, with `InComment` (D-006). Tags in comments are **not** reported (assumed, not probed).
- **Headings in quotes or lists** are blocks, but they aren't in `Doc.Headings` and don't start sections.
- **Tag characters** follow the probe results:
  - A tag ends at whitespace, at ASCII punctuation other than `-` `_` `/`, or at U+2000–206F and U+2E00–2E7F.
  - A `#` right after a letter, a digit or `(` doesn't start a tag, unless it directly follows another tag (`#a#b`).
- **Task items:** a task item's text is scanned after its checkbox, so `[` in `[[] …` is the box, not a link. The fuzzer found this case.
- **Block IDs:** a lone `^id` line after a paragraph labels nothing. Obsidian's behaviour there wasn't probed. After a list, quote, callout or table, it labels that block (D-006).
- **Other choices:** `webm` embeds and the size `|W` apply only to image extensions. Markdown destinations are percent-decoded, and invalid escapes are kept as written.

**Properties (`vault/props`)**
- **Patch range:** a key's lines run from its line to the next key's line, minus trailing blank lines and column-0 comments, which belong to the next key.
- **Self-check:** every patch is re-read before it is returned. It is refused unless every other property keeps its exact bytes and value, and the target appears once. The fuzzer drove this design: it found bare CRs, `---` document markers inside frontmatter, indented root mappings and explicit `?` keys.
- **Refused as unpatchable:** frontmatter with bare CRs, document markers, indented or flow-style root mappings, invalid YAML, and JSON (P-JSON-01).
- **Property names:** at most 256 bytes of valid UTF-8, with no control characters, and not starting with `#`. Values must be valid UTF-8.
- **Writing values:**
  - Strings are plain when that reads back unchanged; otherwise yaml/v3 quotes them. Values containing `[[` or line breaks are always double-quoted.
  - Lists keep an existing list's style (its block item prefix, or flow), and new lists use `  - `.
  - A `Datetime` value without seconds gets `:00`.
  - Overwriting a nested (map) value is refused. Other keys around it are fine.
- **Tags:** frontmatter tags come only from the exact key `tags`, matching D-006, where the singular `tag` gave none. Property reads still treat `tag` as `tags` (P-KEY-02).

**Edits (`Vault.Edit`)**
- **Heading paths:** `A#B` means a heading `B` with `A` among its enclosing headings, in order. An exact match is tried first, then a unique case-insensitive one. Ambiguity is refused with the candidates and their lines.
- **`replace_section`** replaces the whole section body, subsections included, and keeps the blank lines around it.
- **`insert_after_heading`** inserts after the heading's leading blank lines.
- **`replace_text`** matches only in the body, never the frontmatter. It needs at least one match, and the count must equal the expected count.
- **`replace_block`** keeps the ` ^id` marker. Block IDs are compared case-insensitively.
- **No line-ending normalisation:** edits and property writes don't normalise line endings, so mixed-ending files keep every byte outside the edit. `Replace`, which rewrites the whole file, still converts to the file's style (V-WRITE-05).
- **Spec additions:** V-EDIT-01..03 were added for the library edit ops, which CLAUDE.md puts in Phase 2. The owner approved this with the Phase 2 plan.

## D-010 Phase 3 choices: links and search (2026-10-25)

**Excluded files (assumed)** (V-SRCH-03)
- A `userIgnoreFilters` entry wrapped in slashes (`/re/`) is a regular expression, matched anywhere in the path with Go's `regexp`. An entry that doesn't compile is ignored.
- Any other entry is a case-sensitive path prefix, which covers the observed folder form `pr06/excluded/`.
- Only the folder form was probed. The rest follows existing Obsidian tools.

**Resolver** (L-RES-01..05)
- **Order:**
  1. An empty target is the note itself.
  2. A target starting with `./`, `../` or `/` is a path relative to the note's folder or to the vault root. Nothing else is tried, and a path that climbs out of the vault is unresolved.
  3. Otherwise, an exact vault path wins, then a path relative to the note's folder.
  4. Otherwise, every file whose path ends with the target at a folder boundary is a candidate. The one in the note's folder wins, then the shortest path, and a tie is ambiguous (D-006).
- **Matching:** it ignores case, and `.md` is optional. `X.md` is preferred over a file named exactly `X`.
- **Candidates:** any visible file, attachments included. Hidden and denied files are never candidates, but excluded files are.
- **Canvas file nodes** resolve the same way. Their `file` is normally a full vault path.

**Index**
- **Lazy, incremental:** both indexes are built on first use. Each query re-lists the vault and re-reads only files whose modification time or size changed. If a file is added or removed, every link is re-resolved, which is cheap: it's map lookups.
- **10k notes:** first build plus query takes about 3 s for links (400k links) and about 1 s for search; warm queries take 40–60 ms (Apple silicon).
- **Canvas:** decoded minimally (`file` nodes only) until Phase 6 adds validation. An invalid canvas gives no links.
- **Backlinks and orphans [policy]:** a note's links to itself don't count as backlinks or stop it being an orphan. Orphans are files of any kind with no incoming link from another file.
- **Unresolved:** ambiguous links aren't counted as unresolved. They carry their candidates instead.
- **Frontmatter links:** these carry the line of their value. Their spans are offsets into the value, so a Phase 5 rewrite patches the property, not the note bytes.

**Search [policy]** (V-SRCH-01..05)
- **Tokens:** runs of letters, marks and digits, lower-cased, without stemming. Han, Hiragana and Katakana are one token per character.
- **What's indexed:** the whole file, including frontmatter, plus the note's name, which counts twice.
- **Queries:**
  - every word must match, and a `"quoted phrase"` must appear verbatim, ignoring case
  - ranking is BM25 (k1 = 1.2, b = 0.75), then path
  - a query with no text lists matches by path
- **Snippets:** up to 3 matching lines per result, each about 160 bytes with `…` where it's cut.
- **Pages:** default 20 results, maximum 100. The cursor is an offset into the result list.
- **Tag filter:** matches inline and frontmatter tags. A tag filter also matches nested tags.
- **Property filter:** matches a scalar or any list item, ignoring case; deprecated keys are read as plural. An empty value only needs the property.
- **Tag listing:** counts notes. A parent's count includes notes that have only a nested tag.

## D-011 Phase 4 choices: MCP server v1 (2026-10-26)

**Dependencies**
- **`github.com/modelcontextprotocol/go-sdk` v1.8.0** is the official SDK (CLAUDE.md), and the latest release (2026-09-04). It is pure Go and its transitive modules are pure Go; `make verify-static` passes.
- **`golang.org/x/sys` was raised to v0.44.0.** The SDK pulls in v0.41.0, and govulncheck flagged a Windows-only advisory (GO-2026-5024) in that version. We never build for Windows, but the bump keeps the scan clean.

**Config** (X-SRV-02)
- **Vaults** are positional args. `NAME=FOLDER` names a vault, when NAME contains no slash; otherwise the folder name is used. Duplicate names, compared ignoring case, are an error.
- **The first vault is the default** when a tool omits `vault`, and an unknown name is an error.
- **Flags win over env.** Flags must come before the vault args (Go `flag` rules).
- **Journal:** `OBS_MCP_JOURNAL` takes `on` (`~/.local/state/obs-mcp/journal`), `off`, or an absolute folder. The 500 MB cap is fixed. Read-only mode opens no journal.

**Output** (X-SRV-06, X-SRV-07) [policy]
- **Untrusted text:** each result is a short line of server text, then the vault-derived text in one or more blocks:
  - the block starts `<<<NOTE-CONTENT nonce=N label>>>` and ends `<<<END NOTE-CONTENT nonce=N>>>`
  - N is 16 random hex digits, new for each call, so a note can't end the block early
  - paths, link text, snippets, property values and diffs all count as vault text
- **Structured content** carries paths, etags, counts, cursors, properties and outline. Note bodies, snippets and diffs appear only in the text.
- **Page sizes:**
  - listings default to 50 items, with a maximum of 200, and each page's text is capped at 64 KB
  - search keeps the library's page size of 20, with a maximum of 100
  - a cursor is an offset into the result list
- **Read caps:** `read_note` returns at most 256 KB, cut on a line boundary, with a hint to continue with `start_line`. `read_notes` takes at most 20 paths and 512 KB in all.
- **Section and block reads:** `read_note` by heading includes the heading line and the section's trailing blank lines. By block, it returns the block's text and, for a standalone ID line, that line too.

**Tools** [policy]
- **`.md` only:** `create_note`, `edit_note` and `replace_note` write notes only. Canvas and Bases get validated tools in Phase 6.
- **`list_trash`** is registered in read-only mode too: it only reads.
- **Annotations:**
  - read tools: `readOnlyHint` true, not destructive, idempotent
  - `create_note` and `restore_from_trash`: not destructive
  - every other write tool: destructive
  - no write tool is idempotent
  - every tool: `openWorldHint` false
- **`delete_note`** returns the path inside `.trash`, which is what `restore_from_trash` and `list_trash` use.
- **`orphans`** has a `notes_only` filter. By default it lists files of any kind (D-010).

**Errors** (X-SRV-10)
- Typed errors become tool errors whose text ends with `Hint: …`:

  | Error | Hint |
  |---|---|
  | conflict | call `read_note` again and retry with the new etag |
  | file already exists | read it, then edit or replace it with its etag, or pick another path |
  | missing etag | read the note first |
  | not found | check the path with `list_notes` or `search` |
  | path refused | the path rules |
  | ambiguous link | the candidates |
  | edit target | the candidates, or the tool that lists headings and block IDs |
  | property type | the type hint from `props` |

**Logging** (X-SRV-01)
- One INFO line per tool call: tool, vault, path, duration and ok/error.
- The arguments, which can hold note text, are logged only at DEBUG.

**Commits:** the read and write tools landed in one commit (`feat(mcpserver): …`), not two. They share the server core, and splitting them wouldn't leave a commit that builds.
