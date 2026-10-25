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
