# Go.Git 1.5.2 — Shape

The interface SmartGit users expect: its menu layout, a configurable toolbar,
an index editor, a Branches pane menu, bisect steps and a built-in console.
Plus sparse checkout with partial clone, and the fix for branch switching that
refused to work on Windows.

## Menus and toolbar

- The main menu now follows SmartGit's layout: **Repository, Edit, View,
  Remote, Local, Branch, Query, Tools, Window, Help**. Entries the core cannot
  do yet are shown disabled rather than hidden.
- The default toolbar matches SmartGit's and is built from a list in
  `config.toml` instead of being nailed into the markup. Pull, Sync, Push,
  Save/Apply Stash, Log and Git-Flow carry drop-down menus.
- A "Configure Toolbar" window: two lists, rows reordered by dragging,
  separators and stretches, captions under the icons, reset to the default.

## Index editor

- A window with three sides — HEAD, the index and the working copy — edits the
  index directly and leaves the working copy alone. Checked against
  `git diff --cached`.

## Branches pane

- A "≡" menu in the pane title: sort by name, by name with numbers in reverse
  order, or by commit time; grouping by path; Git-Flow sections for remote
  branches; select obsolete local branches. The choices live in `config.toml`.
- A local branch with an upstream shows how far it has drifted: `↑3 ↓1`. The
  counting runs in the background through the `commit-graph`, so the interface
  never waits for it.
- In Git-Flow Light the base branch stands above the sections, as in SmartGit.

## Bisect

- The good, bad and skip steps, in the core and in the interface: a start
  dialog, banner buttons with the revisions left and the steps to expect, and
  the message naming the first bad commit. The `BISECT_*` files and
  `refs/bisect/*` match git byte for byte, so `git bisect log` and
  `git bisect replay` read ours.

## Built-in console

- A console window with command history and git's own command-line parsing.
  status, log, branch, checkout, switch, add, reset, commit, diff, fetch,
  pull, push, merge, rebase, stash, tag, remote, config and submodule run on
  our core, with no system git involved. Commands we do not carry say so.

## Sparse checkout and partial clone

- The `sparse-checkout init / set / add / list / reapply / disable` commands,
  including cone mode and the SKIP_WORKTREE flag in the index.
- `clone --filter=blob:none`: the promisor remote is recorded, missing objects
  are fetched on demand, and the clone dialog offers it.

## Fixes

- **Branch switching refused to work on Windows.** Git keeps its machine-wide
  settings next to the installation (`etc/gitconfig`), and that is where
  `core.autocrlf=true` lives. We did not read that file, so we never stripped
  the CRLF before comparing, and every text file looked modified: a clean
  working copy produced hundreds of files "standing in the way". Both system
  files are now read, in git's own order.
- Submodules under 8.3 short paths were given the wrong `core.worktree`.
- Blame and Investigate were disabled on the toolbar and in the menu although
  they worked from the context menu.
- Checking for updates no longer spends the GitHub API quota: the public
  release feed is read first and the API is only the fallback; a spent quota is
  named as such instead of a bare "403".
- The blame window is tighter: no alternating background, no grid lines, the
  author as a coloured badge with initials like the journal, the commit hash
  opens that commit's changes, and the window can be resized.
- The window no longer hangs in "View → Theme": the engine fixed the deadlock
  and our workaround is gone, so context menus are native again and are not
  clipped by the window edge.
