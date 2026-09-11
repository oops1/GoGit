# Go.Git 1.3.3 — System colours and a cleaner push

The colours of your desktop, one look for every dialog, an attribution ban for
pushes, and three network fixes that made the journal lie.

## Look

- Every dialog is painted by one style: fields, lists and buttons look the same
  everywhere, the main button carries the accent, and the colours come from the
  theme instead of constants.
- The system accent is followed: Go.Git reads the shades Windows keeps in
  `AccentPalette` (and the accent in KDE's `kdeglobals`) and uses the same ones
  the system does — the light shade on a dark theme, the dark one on a light
  theme — for selection, focus and main buttons. A change of accent is picked up
  while the app runs. The window background stays neutral, as it does in
  Windows' own apps.
- A selected row stays readable on any accent: the selection fill moves away
  from the colour of the text.
- The repository settings window follows its mockup: sections with headings, a
  form that scrolls, and a footer with the hint and the buttons.
- The About window is tightened to its content.

## Journal

- The people a commit credits in its trailers — `Co-authored-by`, `Helped-by`,
  `Reviewed-by` and the rest — are shown next to the author: grey squares with
  initials in the short form, names after a comma in the full-name form.

## Attribution ban

- A new git setting, on by default. Before a push, the commits about to leave
  are checked: each needs an author and an email, and the trailers that credit
  somebody else are taken out of your own commits. Commits the server already
  has are never touched.

## Fixes

- After a push or a fetch in a repository where git kept reflogs, the
  remote-tracking branch was not updated: commits stayed "unpushed" and the ↑N
  counter never went down, even after a restart.
- After a pull the journal did not show the new commits until a restart: the
  object database did not see a pack written after it was opened.
- Auto-fetch failed with "Access is denied" every five minutes when the server
  sent the same pack again.
- A menu command that hangs for more than eight seconds leaves the stacks of
  every goroutine in the application log.

## Install

Download the archive for your system, unpack it and run the binary. Nothing else
has to be installed: the git implementation is inside.

- `gogit-v1.3.3-windows-amd64.zip`
- `gogit-v1.3.3-linux-amd64.tar.gz`
- `gogit-v1.3.3-linux-arm64.tar.gz`

`SHA256SUMS` next to the archives carries their checksums.
