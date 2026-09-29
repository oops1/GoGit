# Go.Git 1.6.1 — The diff of a large file

Comparing a large file no longer freezes the window, memory goes back to the
system, and Windows gets an installer.

## The diff of a large file

- Opening the diff of `swagger.json` (30 000 lines) froze the window and pushed
  the process past 2.7 GB. A heap profile taken from the running program named
  the cause: **a single 1.275 GB buffer** inside the engine. The compare pane
  draws a card under a block of lines and asks for a soft shadow under it, and
  the engine allocated a bitmap the size of the whole card — for a 30 000 line
  file that is a canvas 540 000 pixels tall, on every repaint. Fixed in the
  engine (headless-gui v3.26.0, GG-92): the shadow is built from the visible
  part only, the same card costs 1 MiB instead of 1.275 GB, and the picture is
  unchanged.
- A hunk line no longer holds the whole file: until now a single changed line
  kept both versions of the file in memory, because it was a substring of the
  file's text. On a commit with 810 files this frees tens of megabytes.

## Memory

- The program gives unused memory back to the system and collects the garbage
  of a finished operation while it idles, instead of keeping it until the next
  heavy one. The collector has a 2 GiB limit, which the standard `GOMEMLIMIT`
  overrides.
- When memory crosses a threshold, a heap profile is written beside the log
  (`heap-<date>-<time>.pprof`) — that is how the freeze was found.

## Windows installer

- The release now ships an MSI next to the archive: a wizard with a welcome
  page, the licence and a folder of your choice, a Start menu shortcut, removal
  through "Programs and Features" and a handler for `git://` links. Its
  checksum is in the same `SHA256SUMS`.

## Settings

- The colour fields no longer turn "the colour of the theme" into a chosen one
  when the window opens: the engine learned a quiet setter (GG-91) and the
  workaround is gone.
