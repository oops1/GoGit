# Go.Git 1.2.0 — The settings window

The settings live in one window now, built the way a desktop application is
built: a navigation column down the left side, the section on the right, and
the window's own title bar carrying the search field and the window buttons.

## One window instead of four

- A navigation column with an icon per section runs the full height of the
  window. The «≡» button in the title bar folds it into a strip of icons and
  gives the freed width to the content.
- The search field sits in the title bar. It searches parameter names, their
  explanations, group titles, current values, credential resources and SSH
  hosts; each section shows how many settings matched, and an empty result says
  so instead of showing a blank page.
- Every section is laid out on one grid: labels, fields and the grey
  explanations line up, and the rarely touched git parameters are folded into
  «Advanced settings».
- A long section scrolls instead of stretching the window.
- «Save» stays disabled until something is changed, and closing with unsaved
  changes asks what to do with them.
- The window resizes: tables stretch, long values are cut with an ellipsis, and
  nothing overlaps at the smallest size.

## Credentials and keys

- Saved credentials and SSH hosts are tables with a status dot on every row —
  stored, authorisation required, error — and Add, Edit and Remove beside them.
- Adding and editing happen in their own window rather than in a form under the
  table.
- «Test connection» really connects: it asks the remote for its refs over the
  same transport that fetch uses, and reports how many refs answered or why the
  connection failed. «Check key» reads and parses the key file, with its
  passphrase when the key is encrypted.
- The chosen password source is visible at once: the built-in store shows its
  path and how its key is protected, the system git shows which helpers the open
  repository would use.

## Fixed

- The system git credential helper is found even when it is not on PATH: next to
  the git binary itself (`mingw64/bin`, `libexec/git-core`) and in the usual
  install locations. On Windows the call used to fail and Go.Git asked for the
  password itself, while git from the console was answered silently by the
  helper.

## Also

- An About window with the version, platform, architecture, git engine and GUI,
  and links to the project and its licence.
- Update checking: an entry in the Help menu, and an automatic check on start
  once every three days. A failed check is not recorded, so the next start tries
  again.
- Tags in the branches panel fold into a tree by version number, with the three
  newest left in plain sight.

## Install

Download the archive for your system, unpack it and run the binary. Nothing else
has to be installed: the git implementation is inside.

- `gogit-v1.2.0-windows-amd64.zip`
- `gogit-v1.2.0-linux-amd64.tar.gz`
- `gogit-v1.2.0-linux-arm64.tar.gz`

`SHA256SUMS` next to the archives carries their checksums.
