# obsidian-mcp

`obs-mcp` is an MCP server that lets Claude read, search, edit and reorganise your Obsidian vaults. It works on the vault folders directly, so Obsidian doesn't need to be running.

It ships in two pieces:
- **Server:** one static binary, with nothing else to install.
- **Plugin:** three skills that teach Claude to use the server carefully.

## Why it's safe to point at your notes

- **No lost updates.** Every read returns an etag, and every change to an existing file must quote it. If you edited the note in Obsidian since Claude read it, the change is refused and Claude re-reads.
- **Small edits.** Edits patch only the bytes they change: a section, a block, a property's lines, one canvas node or one base entry. The rest of the file stays byte for byte, line endings and all.
- **Links keep working.** Moving or renaming a note, folder, heading, block ID, tag or property rewrites the links that would otherwise break. Ambiguous links are reported, never guessed, and multi-file changes apply all or nothing.
- **Preview and undo.**
  - Every write has a dry run that shows the diff.
  - Deletes go to the vault's `.trash` folder.
  - `undo_last_write` reverses the last change, including a whole folder move.
- **A sandbox.** Paths can't leave the vault, follow symlinks, or reach `.obsidian`, `.git`, `.trash` or other hidden folders. You can hide more paths, such as `Journal/**`, from every tool.
- **No code execution and no network.** The server never runs Obsidian commands, plugins, Templater, scripts or URIs. Note text is passed to Claude as clearly marked data, and Claude is told never to follow instructions found in it.

## Install

Binaries are built for macOS on Apple silicon and for Linux (amd64 and arm64). There are no published releases yet; build them with `make mcpb plugin` (see [Building](#building)).

### Claude Desktop (macOS)

1. Build `dist/obs-mcp-darwin-arm64.mcpb` with `make mcpb`.
2. Open it with Claude Desktop: double-click it, or install it from the Extensions settings.
3. Fill in the settings:

   | Setting | Value |
   |---|---|
   | **Vault folders** | one or more vault folders; the first is the default |
   | **Read-only** | offer only the reading tools |
   | **Hidden paths** | comma-separated globs no tool can see, such as `Journal/**,Private/**` |
   | **Keep undo history** | on by default; keeps previous versions of changed files, up to 500 MB, in `~/.local/state/obs-mcp/journal` |

4. **Turn the extension on** in Settings › Extensions. Desktop doesn't enable an extension that was installed before its required settings were filled in, so after choosing the vault folders, check that the switch is on.

### Claude Cowork

1. Install the Desktop extension above; it supplies the server.
2. Build `dist/obsidian-vaults-plugin.zip` with `make plugin`, then upload it in Customize › Plugins › Upload. It holds only the skills.

> Cowork tasks use the server from the Desktop extension (checked 2026-10-31), so the extension must be installed and switched on.

### Claude Code

```sh
claude mcp add obs-mcp -- /path/to/obs-mcp /path/to/vault
```

- On Linux use `obs-mcp-linux-amd64` or `obs-mcp-linux-arm64`.
- **Several vaults:** pass several folders. Use `NAME=FOLDER` to give one a name.
- **Skills:** copy the folders in `plugin/cowork/skills/` into `~/.claude/skills/`.

## Configuration

Flags win over environment variables.

| Flag | Environment | Meaning |
|---|---|---|
| `VAULT...` | | vault folders, or `NAME=FOLDER`; the first is the default |
| `-read-only` | `OBS_MCP_READ_ONLY=true` | register no write tools |
| `-deny GLOBS` | `OBS_MCP_DENY` | comma-separated globs hidden from every tool |
| `-journal on\|off\|DIR` | `OBS_MCP_JOURNAL` | undo history (default on, 500 MB cap) |
| `-log-level LEVEL` | `OBS_MCP_LOG_LEVEL` | `debug`, `info`, `warn` or `error`; logs go to stderr, one line per tool call |

## Tools

| Group | Tools |
|---|---|
| Discover | `list_vaults`, `list_notes`, `get_note_info` |
| Read | `read_note` (whole, by heading, by block ID or by lines), `read_notes` |
| Search | `search` (BM25 with tag, property, path and date filters), `list_tags`, `find_by_property` |
| Links | `backlinks`, `outgoing_links`, `unresolved_links`, `orphans`, `resolve_link` |
| Write | `create_note`, `edit_note`, `replace_note`, `set_properties`, `remove_properties` |
| Trash | `delete_note`, `restore_from_trash`, `list_trash` |
| Organise | `move_note`, `rename_note`, `move_folder`, `create_folder`, `rename_heading`, `rename_block`, `rename_tag`, `rename_property` |
| Daily notes and templates | `open_daily_note`, `append_to_daily_note`, `list_templates`, `create_from_template` |
| Attachments | `attach_file` |
| Canvas | `read_canvas`, `edit_canvas`, `create_canvas` |
| Bases | `read_base`, `create_base`, `edit_base` |
| Undo | `undo_last_write` |

## Limits

- **Bases** are read, checked and edited, but their queries are never run; only Obsidian can evaluate them.
- **Templates** fill in only `{{title}}`, `{{date}}` and `{{time}}`, with optional Moment.js formats. Templater code is copied as text.
- **Platforms:** no Windows or Intel Mac builds.
- **Sync:** vaults are treated as plain folders. Edits that a sync service makes are caught by the etag checks like any other outside edit.

## Building

You need Go (see `go.mod`). Building the Desktop extension also needs `npx`.

| Command | Does |
|---|---|
| `make lint` | gofmt, vet, staticcheck, golangci-lint and govulncheck |
| `make test` / `make test-race` | unit tests; the second with the race detector |
| `make build` | static, CGO-free binaries in `dist/` |
| `make mcpb` | packs `dist/obs-mcp-darwin-arm64.mcpb` with the pinned `@anthropic-ai/mcpb` CLI |
| `make plugin` | zips the skills plugin to `dist/obsidian-vaults-plugin.zip` |
| `make verify-static` | checks every binary, including the one inside the `.mcpb` |

The design notes are in `docs/`:
- `SPEC_CONTRACT.md`: the numbered requirements, each named by a test
- `DECISIONS.md`: why things are the way they are
- `obsidian/` and `jsoncanvas/`: the source documentation everything is built against
