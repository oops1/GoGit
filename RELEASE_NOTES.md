# Go.Git 1.3.0 — Worktrees and the repository tree

This release is about getting around your repositories: linked worktrees
implemented in our own git core, groups arranged by dragging, context menus
where you expect them, and settings that belong to a single repository.

## Worktrees

- Add, list, remove, prune, lock and move — written in Go, without running the
  system git. Git opens the worktrees we create, and we read the ones git
  created.
- «Add worktree…» is a window of its own: the directory with a browse button,
  three modes (a new branch, an existing branch, no branch at all), the start
  point, and «leave the directory empty». The directory is proposed next to the
  repository and named after the branch, and stops following the branch as soon
  as you type a path yourself.
- The request is checked before anything happens: a directory that is not
  empty, an invalid branch name, a branch that already exists, a branch another
  worktree already holds.
- «Remove worktree» asks first, and asks a second time — about `--force` — when
  the worktree has uncommitted changes. «Prune obsolete worktrees» shows the
  list of records it is about to drop before it drops them.
- Worktrees created outside the application appear in the tree under their
  repository when it is opened.

## The repository tree

- Repositories and groups are rearranged by dragging: into a group, or next to
  another node. Worktrees stay with their repository — they are not group
  members.
- Collapsed groups stay collapsed across restarts.
- Context menus on the tree and on the tables: open, show in the file manager,
  open in a terminal, copy the path; the file table adds staging and discard,
  the journal adds copy hash and copy message.
- Searching for repositories scans a directory for `.git`, lists what it found
  and adds the selected ones in one go.

## Repository settings

- Every repository has its own settings window: the name in the tree,
  `user.name` and `user.email`, the default remote, the pull strategy and
  automatic fetch.
- The settings are written as git's own keys — `user.*`, `remote.pushDefault`,
  `pull.rebase`, `pull.ff` — so the system git reads the same configuration.
  «As in the settings» removes the key instead of writing an empty value.
- The default remote and automatic fetch of a repository win over the global
  ones, so a noisy repository can be silenced on its own.

## Also

- The About window was redrawn to the mockup: sizes, paddings and alignment
  match it pixel for pixel.
- Pane title bars are quieter — they no longer pull attention away from the
  content.
- A commit view shows every file of the commit, and a button with an icon leads
  back to the working copy.

## Fixed

- Worktree paths are compared through their resolved names: a Windows short
  name (`RUNNER~1`) is no longer taken for a different directory.
- A data race is gone: the diff view read the open worktree while another
  thread was replacing it.
- A repository can no longer be nested inside another repository — the registry
  checks that the new parent really is a group.
- Expanding a repository that has worktrees shows its directories again.

## Install

Download the archive for your system, unpack it and run the binary. Nothing else
has to be installed: the git implementation is inside.

- `gogit-v1.3.0-windows-amd64.zip`
- `gogit-v1.3.0-linux-amd64.tar.gz`
- `gogit-v1.3.0-linux-arm64.tar.gz`

`SHA256SUMS` next to the archives carries their checksums.
