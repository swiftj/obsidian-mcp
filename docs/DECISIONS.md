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
