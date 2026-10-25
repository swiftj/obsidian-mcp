# Obsidian changelog excerpts, 1.12–1.15 (desktop)

These are entries from https://obsidian.md/changelog/ that touch the CLI, Bases, Canvas, links, properties or file handling. They were fetched 2026-10-25 and are quoted verbatim. Each section heading gives the release page path.

Status at fetch time:
- **1.15.0** is *Early access* (Catalyst) only.
- The latest **public** release is **1.14.5** (2026-10-22).
- The owner runs 1.14.4.

UI-only fixes (styling, focus, pop-out windows, RTL) are left out.

## 1.12.0 (Early access): `/changelog/2026-02-10-desktop-v1.12.0/`
- Added a command line interface that lets you control Obsidian from your terminal for scripting, automation, and integration with external tools.
- When deleting a file, a new prompt will ask if you would like to also delete its attachments. A new setting in Files & Links allows you to configure this behavior (choose “Always”, “Ask every time”, or “Never”).
- Image size is no longer treated as display text when dragging a link.
- Backlinks in Canvas files are now detected. They are now shown in the backlinks view, and counted as links in the Graph view.

## 1.12.1 (Early access): `/changelog/2026-02-10-desktop-v1.12.1/`
- CLI: If the vault parameter is included, it must be the first argument.
- The daily:prepend CLI command now inserts content after the frontmatter instead of at the very beginning of the file.

## 1.12.2 (Early access): `/changelog/2026-02-18-desktop-v1.12.2/`
- Added rename command. *(CLI)*
- CLI commands now default to silent operation and doesn’t expect an active file by default.
- file.linksTo() treats files embedded within a Canvas as links. *(Bases)*
- CLI: Fixed erroneously operating on the current vault when the specified vault is not found.

## 1.12 (Public): `/changelog/2026-02-27-desktop-v1.12.4/`
- This release introduces the Obsidian CLI, a command line interface that lets you control Obsidian from your terminal for scripting, automation, and integration with external tools.
- Added detection for backlinks in Canvas files. They now appear in the Backlinks view, and count as links in the Graph view.

## 1.12.6 (Early access): `/changelog/2026-03-18-desktop-v1.12.6/`
- Changed the CLI socket file to a hidden dotfile on macOS and Linux.

## 1.12.7 (Public): `/changelog/2026-03-23-desktop-v1.12.7/`
- The Obsidian Installer is now bundled with a new binary file for using the CLI. This new method replaces the old method of calling the Electron binary, resulting in significantly faster terminal interactions. Requires downloading the latest installer.

## 1.13.0 (Early access): `/changelog/2026-05-28-desktop-v1.13.0/`
- Note Composer: When extracting a section from a file, links in that section will now be rewritten to be relative to the new location.

## 1.13.2 (Early access): `/changelog/2026-07-14-desktop-v1.13.2/`
- Reading mode now supports external images with an image size and no alt text (e.g. `![200](https://www.path.to/image.png)`).
- Bases: “File links” no longer show the image size as the image display name.

## 1.13.5 (Early access): `/changelog/2026-08-05-desktop-v1.13.5/`
- Fixed dragging links out of an embedded Base (e.g. `![[file.base]]`) linking to the Base file and not the file you’re dragging.

## 1.14.0 (Early access): `/changelog/2026-09-02-desktop-v1.14.0/`
- Bases: Fixed the “+” button not generating correct frontmatter for unresolved links, e.g. `file.hasLink("[[unresolved]]")`.

## 1.14.2 (Early access): `/changelog/2026-09-15-desktop-v1.14.2/`
- The editor link suggestions (e.g. `[[`) and the quick switcher no longer switch to a simplified algorithm in vaults with more than 10,000 files. Fuzzy matching is now always used.
- Editor: Fixed clicking on collapsed reference links (e.g. `[link][]`) going to the wrong destination.

## 1.14.3 (Early access): `/changelog/2026-09-29-desktop-v1.14.3/`
- Bases: Added a Group menu to the toolbar for choosing the property to group by. Use it to reorder, hide, and add groups by hand in any view type.
- Canvas: Fixed a card narrowed to a heading or block still showing that section after using Swap file… to pick the same file.

## 1.14.4 (Early access): `/changelog/2026-10-01-desktop-v1.14.4/`
- Bases: A base made with “Create new base” now names its first view “View” instead of “Table”.
- Bases: Fixed a base failing to load when a view’s groupOrder is not a list.

## 1.14 (Public): `/changelog/2026-10-05-desktop-v1.14.4/`
- This update introduces support for opening individual Markdown files, a kanban view for Bases, and highlight colors.
- Added the “Open file from outside the vault…” command. Pick a Markdown file on your computer and view it in the app.
- Image embeds and links to other local files resolve relative to the Markdown file’s folder. *(In context, this applies to files opened from outside the vault.)*
- Bases now has a kanban view. Organize your notes into columns and drag cards between groups. You can also reorder and hide groups in any Bases view using the new Group menu.

## 1.15.0 (Early access): `/changelog/2026-10-22-desktop-v1.15.0/`
- URI/CLI: Added commands to append or prepend to a heading, and control for line breaks around appended/prepended content.
- Bases: Added dragging to reorder filters, and a menu on each filter row to add filters or move them between All views and This view.
- Fixed page previews and canvas cards of a heading (e.g. `[[Note#Heading]]`) hiding text, or showing it as properties, when the section under the heading starts with a `---` divider.
- Bases: Fixed a filter value with a double quote or a backslash saving as a broken filter.
