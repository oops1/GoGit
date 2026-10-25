# Go.Git 1.7.1 — Sharper text, faster big working trees

Small text is sharper, the status of a big working tree takes half the time, and
opening a repository no longer holds up the interface.

All numbers below were measured on Windows with a 20-thread processor.

## Sharper text

- Small text is drawn with vertical hinting and a contrast boost for thin strokes:
  the heights of lowercase and capital letters land on whole pixels, and dark text
  on a light background looks denser. Line widths and layout do not change.
  Subpixel smoothing stays off because it is useless in a remote session. The
  headless-gui engine is updated to v3.43.0 for this.

## Big working trees

- Status of a working tree with 50 thousand files, unchanged files included (the
  way the Files pane asks for it), takes 53 ms instead of 103 ms and 41 MB instead
  of 122 MB. File attributes come from the cache without rebuilding the rule stack,
  and for every file only the question of whether it needs an external filter is
  asked.
- A directory with tracked files is read once per scan: the search for untracked
  files reuses the listing that was already read. Status of 50 thousand files takes
  30 ms instead of 44 ms.
- Opening a repository with 80 thousand files holds up the interface for 19 ms
  instead of 250–440 ms. The working tree watcher takes its first snapshot in the
  background, and the status scan waits for it so that no change made right after
  opening is lost. A new set of ignored directories reaches the running watcher
  instead of restarting it with a full walk of the tree.

## Journal, Blame and network

- Loading the next page of the journal no longer drops the selected commit or
  rebuilds the whole table.
- In Blame a commit opens with a single click on its link, and the cursor turns
  into a hand over it.
- A secure connection over a slow network waits a minute instead of 15 seconds, and
  the server response timer starts only after the request is sent. When the
  connection never comes up, Go.Git says so instead of "context deadline exceeded".
