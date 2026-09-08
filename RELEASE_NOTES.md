# Go.Git 1.3.1 — The commit graph

The journal draws the history now: lanes, dots, ref labels, and the commits that
exist only on this machine. Menus got their icons at the same time.

## The graph

- The graph column draws a lane per line of history: a filled dot for a commit,
  a ring for a merge, a rounded curve where a line moves from one lane to
  another. A lane keeps its colour for as long as it lives.
- The main line stays in the leftmost lane. When two lines meet, the left one
  survives and the one that closes is led into it — the graph no longer drifts
  to the right with every merge, leaving a trail of empty lanes behind.
- The column is exactly as wide as the lanes on screen, and it is recounted
  while scrolling. No space is reserved for a branch that is not in view.
- The history is walked the way git walks it when it draws a graph: no parent
  is shown before all of its children. Walking strictly by date opened a lane
  for every merge commit — merges are made later than the branch they merge —
  and those lanes stayed empty to the bottom of the page.
- The lines between rows are gone: they cut the lanes.
- The lane layout survives paging, so the lines do not break where the next
  page begins. Beyond sixteen lanes the rest collapse into the last one.

## Labels and colour

- Refs are drawn as rounded labels before the message: the current branch
  filled with the accent, other local branches with a quieter fill, remote
  branches grey, tags amber. They are ordered current branch, local, remote,
  tags.
- Commits the server does not have are drawn amber — the dot and the lines. A
  repository with a remote that was never pushed is local all the way down; a
  repository without a remote is not coloured at all, or the colour would mean
  nothing.

## Icons in the menus

- Every menu — Repository, Edit, Remote, View, Help — and every context menu
  now carries an icon per item: 28 drawn for this release plus the toolbar
  icons for the commands both places share. A disabled item dims its icon along
  with its label.

## Engine

- Raised to v3.16.10: an icon in a menu item (GG-53) and the lines, polylines
  and outlines of `AAShapes` in the table cell drawing context (GG-54).

## Install

Download the archive for your system, unpack it and run the binary. Nothing else
has to be installed: the git implementation is inside.

- `gogit-v1.3.1-windows-amd64.zip`
- `gogit-v1.3.1-linux-amd64.tar.gz`
- `gogit-v1.3.1-linux-arm64.tar.gz`

`SHA256SUMS` next to the archives carries their checksums.
