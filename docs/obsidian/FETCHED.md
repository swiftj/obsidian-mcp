# Fetched sources

Fetched **2026-10-25**. The sha256 of every file is also in `docs/SHA256SUMS`; verify with `cd docs && shasum -a 256 -c SHA256SUMS`.

## How the files were fetched

- **Obsidian help:** raw Markdown from `github.com/obsidianmd/obsidian-help`, commit `f333c95d23fc1e30594fa7acb9181f67ab71861c`, path `en/<source path>`.
  - Each file's `permalink` frontmatter matches its canonical URL.
  - Each file was compared **byte for byte** with the Markdown the live site serves through Obsidian Publish (`publish-01.obsidian.md/access/f786db9fac45774fa4f0d8112e232d67/<source path>`). All 18 were identical.
- **JSON Canvas:** `github.com/obsidianmd/jsoncanvas`, commit `456f843cb293df4f4ab1763e22ccb46a80b307c8`, path `spec/1.0.md`. Spot-checked against https://jsoncanvas.org/spec/1.0/.
- **Changelog:** `CHANGELOG-excerpts.md` is a hand-made verbatim extract from the HTML release pages it names. It has no single upstream file.
- `obsidian-flavored-markdown.md` was added beyond the CLAUDE.md list because it is the hub page for OFM.

## Files

| File | Canonical URL | Source path | sha256 |
|---|---|---|---|
| `obsidian/syntax.md` | https://obsidian.md/help/syntax | `en/Editing and formatting/Basic formatting syntax.md` | `38db5d30f637adfcbe212fd8b5616412b93bd8c0387845e5ff5355f5195235bd` |
| `obsidian/obsidian-flavored-markdown.md` | https://obsidian.md/help/obsidian-flavored-markdown | `en/Editing and formatting/Obsidian Flavored Markdown.md` | `da08d3873a6b5a2334d57c1c943a9a1ca6d74991573638b466d3f6ecf9c9cda4` |
| `obsidian/links.md` | https://obsidian.md/help/links | `en/Linking notes and files/Internal links.md` | `4ebf7be1cce4bf31e104b60c8eb2a175f134a79a29e29a16822033cfb5088ce1` |
| `obsidian/embeds.md` | https://obsidian.md/help/embeds | `en/Linking notes and files/Embed files.md` | `5757a6269949f507f3b5f1605ab9bfe81f247738d5b44c3bf096efc6215fbc54` |
| `obsidian/callouts.md` | https://obsidian.md/help/callouts | `en/Editing and formatting/Callouts.md` | `5d12e34b9fb68c6b7ad0ea39d80fb11267b0406cd204f61d4c00f2d5dea9bab6` |
| `obsidian/tags.md` | https://obsidian.md/help/tags | `en/Editing and formatting/Tags.md` | `20214764032cb166d6e13cc39605b654d691f5a17ad70437d6df81e28fc149dc` |
| `obsidian/properties.md` | https://obsidian.md/help/properties | `en/Editing and formatting/Properties.md` | `75c85e422f5e2b17c9c8fe9125b6c9b4159fd6635cefbbef6b72c3272b623db6` |
| `obsidian/advanced-syntax.md` | https://obsidian.md/help/advanced-syntax | `en/Editing and formatting/Advanced formatting syntax.md` | `15ed1c714c0e8a4dd48ac05ce53df787d1eda241c78d8b27a091f142cbe26307` |
| `obsidian/settings.md` | https://obsidian.md/help/settings | `en/User interface/Settings.md` | `b3947392c596720a3e91b85b08a927b2e7e318c01b0a986b65090c656b192029` |
| `obsidian/configuration-folder.md` | https://obsidian.md/help/configuration-folder | `en/Files and folders/Configuration folder.md` | `ab6b05840f1508b621210f2fb74a68199b22a80d0370ecccbb480b5b98a4e9d6` |
| `obsidian/file-formats.md` | https://obsidian.md/help/file-formats | `en/Files and folders/Accepted file formats.md` | `95ede78937600de68ad15ade8cf5044f05261eac9f72187c2880f6a7c71b517e` |
| `obsidian/daily-notes.md` | https://obsidian.md/help/plugins/daily-notes | `en/Plugins/Daily notes.md` | `776472f0c26adcc0c7556b4a7f3b7e3440720b48b724689a0282985fede4c2b8` |
| `obsidian/templates.md` | https://obsidian.md/help/plugins/templates | `en/Plugins/Templates.md` | `abf70b302301e49448c47f4a770d4d466d0946e5dfacb66b36fbd0aed0efbb65` |
| `obsidian/bases-syntax.md` | https://obsidian.md/help/bases/syntax | `en/Bases/Bases syntax.md` | `bacb5c96516390c9f200f607037003da97f8cf4152b2959cbad0e29dfba8c152` |
| `obsidian/bases-create.md` | https://obsidian.md/help/bases/create-base | `en/Bases/Create a base.md` | `bc33bb615dc2156c3b77252eb035e8d50cfc43fc9e6211fbcaa432e66bea671f` |
| `obsidian/cli.md` | https://obsidian.md/help/cli | `en/Extending Obsidian/Obsidian CLI.md` | `884d3f36a30ad2dc08bcdc84c1243e17e255677a2bd4a7a1aa0d8f77939fc012` |
| `obsidian/uri.md` | https://obsidian.md/help/uri | `en/Extending Obsidian/Obsidian URI.md` | `183ba482457be55723c23f3dc568e82e602f27082fc17f33aece38924f7443ed` |
| `obsidian/sync.md` | https://obsidian.md/help/sync-notes | `en/Getting started/Sync your notes across devices.md` | `7ab268aa4d6820a888515d8cab5ad9658e88783e002c6acd74023472ab63acbe` |
| `jsoncanvas/spec-1.0.md` | https://jsoncanvas.org/spec/1.0/ | `spec/1.0.md` | `41d75005394f3ed43a53031ff9d07c5d49c47e897971e7afb2972cc8af67469a` |
| `obsidian/CHANGELOG-excerpts.md` | https://obsidian.md/changelog/ | (hand extract) | `76dbda7231b880bede8b10b00e39da4585115d1c2f3c31dec4ec070eb701da94` |
