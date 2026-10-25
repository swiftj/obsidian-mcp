# Manual probe checklist (Phase 0)

> **Stopped after step 3 (2026-10-25)** at the owner's request. The results and the assumptions made for the remaining steps are in `docs/DECISIONS.md` D-006.

These probes answer the `[probe PR-xx]` questions in `docs/SPEC_CONTRACT.md`, using a **scratch vault** made from `testdata/probes/vault/`. All of its content is made up. `scripts/probe.sh` refuses to run anywhere except under a system `$TMPDIR`. Never open a real vault for this.

**How it works:**
- You do one step in Obsidian.
- Then you run `! scripts/probe.sh snap <label>` in Claude Code.
- The script copies the vault and prints the diff from the previous snapshot. That shows exactly what Obsidian wrote.
- Some steps also run `scripts/probe-dump.js` in Obsidian's developer console. It writes `probe-dump-<n>.json`, a dump of Obsidian's own metadata cache, link resolver and settings, into the vault root.

**The dump script** only reads, apart from writing that one JSON file. It changes the link-format settings in memory to generate sample links, then restores them. It stops if the open vault isn't the probe vault.

Tested on Obsidian **1.14.4** (macOS).

---

## 0. Open the scratch vault
1. Claude runs `scripts/probe.sh setup`, which prints the vault path.
2. In Obsidian, open the vault switcher and choose **Open folder as vault**. Press `Cmd+Shift+G`, paste the path, and open it. Don't change any settings.
3. Run `! scripts/probe.sh snap open`. **(PR-07, PR-06: what Obsidian creates while the vault is open)**

## 1. First dump (default settings) — PR-01/02/03/04/06/09/10/12/13/14/15
1. Run `! pbcopy < scripts/probe-dump.js` to copy the snippet.
2. Click into the **ObsProbe** window first (each vault has its own window and its own console). Then press `Cmd+Opt+I`, open the **Console** tab, and paste with `Cmd+V`. If Chrome asks, type `allow pasting` first, then paste again. Press Enter.
3. Run `! scripts/probe.sh snap dump-defaults`.

## 2. Property types — PR-05
1. Open `pr05/Types.md`.
2. For each property, click the icon left of its name, choose **Property type**, and pick:
   - `p_text` → Text
   - `p_list` → List
   - `p_num` → Number
   - `p_bool` → Checkbox
   - `p_date` → Date
   - `p_dt` → Date & time
3. Add a property `p_picked`, set its type to **Date & time**, and pick any date and time with the picker.
4. Run `! scripts/probe.sh snap types`.

## 3. Files & links settings — PR-06
Open **Settings → Files and links** and do the following:
1. Turn **Automatically update internal links** on.
2. Set **Default location for new notes** to *In the folder specified below*, with the folder `pr06`.
3. Set **Default location for new attachments** to *In subfolder under current folder*, with the subfolder name `att`.
4. Under **Excluded files → Manage**, add the folder `pr06/excluded`. If there is a way to type a pattern, also add `secret`.
5. Run the dump again (step 1.2; the snippet is still on the clipboard).
6. Run `! scripts/probe.sh snap settings`.
7. Set the attachment location to *Same folder as current file*, then run `! scripts/probe.sh snap att-same`.
8. Set it to *In the folder specified below* with the folder `pr06`, then run `! scripts/probe.sh snap att-folder`.
9. Set it to *Vault folder*, then run `! scripts/probe.sh snap att-root`.

## 4. Daily notes and Templates — PR-11
1. Under **Settings → Core plugins**, turn on **Templates**, and **Daily notes** if it's off.
2. In **Templates** options:
   - Template folder location: `pr11/tpl`
   - Date format: `DD.MM.YYYY`
   - Time format: `HH:mm:ss`
3. In **Daily notes** options:
   - Date format: `YYYY/MM/YYYY-MM-DD`
   - New file location: `pr11`
   - Template file location: `pr11/tpl/Daily`
4. Run `! scripts/probe.sh snap plugin-settings`.
5. Run the command **Daily notes: Open today's daily note**.
6. Run `! scripts/probe.sh snap daily`.

## 5. Link-updating renames — PR-01/02/04/10/13/14
Rename each file in the File explorer (right-click → **Rename**). If you're asked whether to update links, choose **Update**.

| File | New name |
|---|---|
| `pr01/a/Tie` | `TieA` |
| `pr01/c/Near` | `NearC` |
| `pr02/Case Target` | `Case Target 2` |
| `pr10/Md Target` | `Md Target 2` |
| `pr13/Heads` | `Heads2` |
| `pr14/Target14` | `Target14b` |

Then run `! scripts/probe.sh snap renames`.

## 6. Alias links — PR-03
1. Open `pr03/src.md` and switch to Reading view (`Cmd+E`).
2. Click **Nick Name**. Note whether it opened `Aliased`, or created or opened a new note. Go back.
3. Do the same with **other alias**.
4. Run `! scripts/probe.sh snap alias-click`, and tell Claude what each click did.

## 7. Canvas — PR-16
1. Open `pr16/Existing.canvas`, drag the **Hello** card a little, and wait about 3 seconds.
2. Run `! scripts/probe.sh snap canvas-edit`.
3. Right-click the `pr16` folder and choose **New canvas**. Then:
   - add a text card containing `x`
   - drag `pr16/Note16.md` from the File explorer onto the canvas
   - connect the text card to the note card
   - create a group around both
4. Run `! scripts/probe.sh snap canvas-new`.

## 8. External writes while open — PR-08
1. Open `pr08/Watch.md` in editing view, and keep it visible.
2. Without typing, run `! scripts/probe.sh ext atomic`. Did a new line appear in the editor, and was there any notice?
3. Without typing, run `! scripts/probe.sh ext inplace`. Same questions.
4. Type `abc` at the end of the note. Then, within a second or two, run `! scripts/probe.sh ext inplace` (have it typed in the terminal ready). What does the editor show now? Is `abc` or the `EXT` line lost?
5. Wait 5 seconds, then run `! scripts/probe.sh snap watcher`. Tell Claude what you saw in steps 2–4.

## 9. Closed vault — PR-07
1. Quit Obsidian (`Cmd+Q`).
2. Run `! scripts/probe.sh snap closed`.

## Afterwards
- Claude records the results in `docs/DECISIONS.md`, then deletes the scratch folder with `scripts/probe.sh clean`.
- Remove the `ObsProbe` vault from Obsidian's vault switcher (**Remove from list**).
