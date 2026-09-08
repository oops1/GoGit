# Go.Git 1.3.2 — Two fixes

A patch release: the repository you had open comes back open, and the diff pane
shows the file you picked.

## Fixes

- The repository that was active when the app closed is opened again at startup.
  Until now it was only highlighted in the tree — the path appeared in the status
  bar and the folders expanded, but branches, journal and working copy stayed
  empty until it was double-clicked.
- The diff pane shows the selected file, not the one that happens to sit at that
  row number. With the file list filtered — by text or by status — grid rows were
  still read as indexes into the unfiltered list, so a different file's diff was
  shown, usually an unchanged one with nothing to display. Files are now matched
  by path.

## Under the hood

- `gitcore/merge` merges one file three ways in the merge, diff3 and zdiff3
  styles, byte for byte with `git merge-file`, conflicts three lines apart or
  less joined the way git joins them. It is the ground floor of the merges in
  1.4.0 and is not reachable from the interface yet.

## Install

Download the archive for your system, unpack it and run the binary. Nothing else
has to be installed: the git implementation is inside.

- `gogit-v1.3.2-windows-amd64.zip`
- `gogit-v1.3.2-linux-amd64.tar.gz`
- `gogit-v1.3.2-linux-arm64.tar.gz`

`SHA256SUMS` next to the archives carries their checksums.
