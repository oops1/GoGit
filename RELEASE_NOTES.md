# Go.Git 1.6.0 — Stability

Compatibility with git from 2.30 to the current release is checked in CI on two
systems, blame of a large file fits in memory, and the program now comes with
installers.

## Compatibility with different versions of git

- The oracle tests run against git 2.30.2, 2.39.2 and 2.47.1 on Windows and
  Linux in a workflow of their own. Working through some sixty differences
  found two real incompatibilities in our transport: the first line of a
  reference advertisement may arrive without a capability list, and git before
  2.31 sends nothing but a flush for an empty repository. Both used to be
  treated as a protocol error, so cloning an empty repository from an older
  server did not work.
- The rest of the differences belong to git itself. The tests now either ask it
  how it behaves or skip the comparison with an explanation instead of failing.

## Blame and diff

- Lines in the diff engine are no longer copies of the text: a file is held as
  one buffer and a line is a range inside it. Blame of a large file takes
  160 MiB in the internal benchmarks instead of 717 and 614 MiB, and a single
  diff became lighter as well (4.1 MiB instead of 4.3, with fewer allocations).
- The cache of restored pack objects grew from 16 MiB to 96 MiB, the default
  git uses for `core.deltaBaseCacheLimit`, and that key is now read from the
  repository configuration. On a 1.4 MB file with three hundred revisions blame
  allocates 680 MiB instead of 1473 MiB.
- The memory budgets are now ordinary tests: blame, diff and a long history
  walk fail if their allocations grow.

## File permissions

- `.git/config` is written the way git writes it: a new file takes the
  permissions of the process mask, an existing one keeps its own. We used to
  force `0600` and took the access away from everyone else on a shared
  repository.
- The `0600` permissions of `config.toml`, `vault.bin` and the vault key file
  are set explicitly and held by tests instead of depending on how temporary
  files happen to be created.

## Interface and settings

- Branches pane: a local branch and its remote are one row with the divergence
  counter, as in SmartGit, instead of two rows.
- Settings and the vault survive a version upgrade: both have a migration point
  and golden checks against files of earlier versions.
- Heap growth is written to the log at thresholds, and `GOGIT_PPROF` opens a
  profile on a loopback address, so a freeze can be studied from facts.

## Installers and documentation

- The packages are built by a workflow of their own: an MSI (WiX) with a
  shortcut, an icon and a `git://` handler, `.deb` and `.rpm` through nfpm, and
  an AppImage. On Linux the protocol association goes through a `.desktop` file
  with `x-scheme-handler/git`.
- `README.en.md` and the user guide `USER_GUIDE.md` are now in the repository.

## Fuzzing

- Twenty-four fuzz targets run on a schedule as separate jobs; a short round
  over all of them finds nothing.
