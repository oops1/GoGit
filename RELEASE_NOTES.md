# Go.Git 1.7.0 — Speed

Status, the journal, branch counters and object reading are noticeably faster on
big repositories, and Go.Git now writes the commit graph itself.

All numbers below were measured on Windows with a 20-thread processor.

## Working tree status

- Status of a repository with 50 thousand files takes 0.12 s instead of 5.1 s.
  Almost all the time went into stating each file through a path that is opened
  component by component. A directory is now read once and its files are described
  from that listing; only small directories and files missing from a listing are
  stated one by one.
- The index is compared with HEAD through the cache tree: if a directory has the
  same tree as in HEAD, it is not read at all. With nothing staged, no tree below
  the root is read.
- Untracked directories are walked in parallel, each read once. With 20 thousand
  untracked files the status takes 23 ms instead of 46 ms, with 20 thousand ignored
  files 24 ms instead of 76 ms. The ignore and attribute matchers no longer wait for
  each other behind a lock.

## Journal and history

- If a repository has no commit graph, Go.Git writes it in the background after the
  repository is opened. With a graph the journal streams its first page instead of
  walking the whole history. The settings `core.commitGraph` and
  `gc.writeCommitGraph` are honoured, and shallow clones are left alone.
- Parsing a commit makes almost four times fewer allocations: a walk over 10 thousand commits goes
  from 330 thousand allocations to 90 thousand and from 12.1 ms to 7.5 ms.
- The journal hands its rows to the grid once per page instead of copying them
  twice: with 100 thousand rows 65 ms instead of 127 ms and 284 MB instead of 635 MB.
- A refresh no longer restarts the journal when no branch, tag or remote moved.

## Branch counters

- The ahead and behind counters are computed once for each pair of branch tips and
  remembered, so a refresh recounts only the branches that moved. All walks of one
  computation share the commits they have parsed, and the counting reuses the open
  repository and its cached commit graph.

## Objects and references

- Pack indexes keep their name table in memory and, together with packfiles, are
  read through a memory mapping on Windows and Linux (with a fallback to plain
  reads). Looking an object up in a pack index takes about 70 ns.
- `packed-refs` is parsed once while the file does not change instead of on every
  miss.
- A missing object no longer rescans the pack directory when it has not changed.
- The pack that answered last is searched first; the decompression buffers are
  pooled; the parsed object cache sweeps dead entries lazily; the raw object cache
  is split into shards so parallel readers stop sharing one lock.

## Diagnostics

- `GOGIT_TRACE_STARTUP=1` writes the startup steps with their time since the process
  start into `gogit.log`: logger opened, application built, repository opened,
  window started, first journal page and working tree status shown.
