# Go.Git 1.0.0

First release of Go.Git — a free desktop git client for Windows and Linux, and a
free alternative to SmartGit. Git is implemented from scratch in pure Go: no
system git is called, no CGO is compiled, and the whole application ships as a
single executable with every resource inside it.

## Working with a repository

- **Repositories panel** — groups, repositories and worktrees. The open
  repository expands into the directory tree of its working copy, read one level
  at a time, and picking a directory narrows the file list to it. The current
  branch is shown next to the name; folders git does not track are greyed out
  and tracked dot-directories are dimmed.
- **Branches panel** — local branches, remotes, tags and stash. The checked out
  branch and the branch a remote HEAD resolves to carry their own icons; the
  symbolic HEAD is not repeated as a separate row.
- **Files panel** — every path with its state, including ignored files and
  tracked files with no changes, exactly as `git status --ignored=traditional`
  reports them. Filter by name, by state (nine toggles that persist across
  restarts), by directory, and with or without subdirectories.
- **Journal** — commit history paged in as you scroll. The author is a coloured
  badge with two initials, drawn once for a run of commits by the same person;
  a setting brings the full name back.
- **Diff** — two panes, side by side, with changed lines highlighted. Selecting
  a commit shows its files and their diff.
- **Write operations** — stage, unstage, discard, commit, create, rename and
  delete branches, switch branches.
- **Long operations** — a modal window with an endless progress bar, a log of
  what is happening and a cancel button.

## Behaviour

- Changes made by another git — a command line, an IDE, another client — are
  picked up automatically. The watcher skips directories git ignores, so an IDE
  writing to `.idea` or a build writing to `output` costs nothing.
- History stops at the commits listed in `shallow`, so the journal works on
  repositories cloned with `--depth`.
- Windows 11 light and dark themes, following the system by default.
- Russian and English out of the box; any other language is a single JSON file.
- Dock panels can be hidden, moved to another edge or torn off, and the layout
  is restored on the next start.

## Not in this release

Network operations (clone, fetch, pull, push), the encrypted credential store,
SSH, merge, rebase, cherry-pick, stash operations and line-level staging. Pull,
Sync and Push open the operation window and say so. They are planned for 1.1.0
and the releases after it — see `docs/RELEASE_PLAN.md`.

## Downloads

- `gogit-v1.0.0-windows-amd64.zip` — Windows 10/11, 64-bit.
- `gogit-v1.0.0-linux-amd64.tar.gz`, `gogit-v1.0.0-linux-arm64.tar.gz` — Linux,
  X11 and Wayland.
- `SHA256SUMS` — checksums for the archives above.

No installer: unpack the archive and run the binary.
