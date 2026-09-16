# Go.Git 1.5.0 — Depth

Staging lines and hunks, stashes, repository maintenance, journal search,
git-flow the way SmartGit does it, hooks, merges and rebases taken all the way
to how git behaves on the awkward cases, a faster blame and a sharper diff,
and a sturdier network and secret store.

## Staging and stash

- Lines and hunks can be picked right in the diff pane and applied to the
  index or the working copy — staging, unstaging and discarding part of a
  file, not only the whole of it. Checked against `git apply --cached` on 11
  kinds of selection in both directions.
- Stash: save, apply, unstage and drop from the Edit menu and from the
  "Stash" node of the Branches panel; `refs/stash` and its reflog match git.
  Switching branches over local changes that are in the way offers a choice —
  carry them along in a stash, bring them over by merging, or overwrite them
  — and remembers what you picked.

## Repository maintenance

- Our own `repack`, `gc`, `prune`, `fsck`, `count-objects` and `commit-graph`
  writing — no system git involved. The reachable object set, the packfiles
  and the commit-graph file are checked byte for byte against the matching
  git commands.

## Journal search

- The journal filter searches by changed content (`-S`, `-G`, with a
  "regular expression" checkbox) and by path — a second row of the filter
  bar, alongside the branch, author and message filters from 1.4.0.

## Git-flow

- Full git-flow in SmartGit's own style: a "Git-Flow ▾" toolbar button and
  the same menu under "Branch → Git-Flow" — Start/Finish for Feature,
  Hotfix, Release and Support Branch, Integrate Develop, a "Configure…"
  dialog with a Light/Full switch and a change-or-switch-off question window,
  and a Git-Flow Light mode with no `develop` branch. The command sequence
  for every branch kind is checked hash for hash against SmartGit's own
  operation log.
- A double click switches branches, and the branch and file context menus
  match SmartGit — including committing changes without staging them first.

## Hooks

- `pre-commit`, `commit-msg`, `post-checkout`, `post-merge`, `pre-rebase` and
  `pre-push` run as separate processes the way git runs them — on Windows
  only extensionless scripts and `.exe`, the way Git for Windows does it.
  The commit and push dialogs show the hook's output and offer to bypass it.

## Submodules and bisect

- `.gitmodules` is checked the way `git fsck` checks it; the commit a
  submodule is checked out at is tracked through conflicts, staging and
  checkout, and a moved submodule shows up in working-copy status the way
  `git status` reports it.
- A bisect started by another git client is detected and shown in a banner
  that can end it the way `git bisect reset` does.

## Merges and history

- Merging honours `merge.conflictStyle`, a path's diff attributes and custom
  version labels in a conflict; directory renames follow
  `merge.directoryRenames`, and rename detection is capped the same way
  git's is.
- Merge, cherry-pick, revert, rebase, applying a stash and git-flow steps now
  stop with a conflict exactly where git stops — on a split renamed
  directory, a path that quietly changed type, and names that differ only by
  case — instead of silently filing something in the wrong place.
- `rerere` tells apart identical-looking conflicts from the same merge; an
  interactive rebase keeps commits that started out empty and skips commits
  already applied upstream, the way git does.

## Blame and diff

- Blame follows only genuine renames onto the blamed path and walks history
  through a date queue — noticeably faster on long histories; the `patience`
  and `minimal` algorithms and an `ignore-cr-at-eol` option were added.
- A path's diff attributes decide what counts as binary and are applied to
  commit diffs; tree comparisons understand git pathspec magic and globs.

## Network

- An SSH connection offers a key from the vault, from ssh-agent, or from an
  identity file, with a dialog for the key's passphrase; `core.sshCommand` is
  parsed, the host key algorithms `known_hosts` lists are honoured, lines it
  cannot parse are skipped, and the OpenSSH agent pipe is used when nothing
  else answers.
- The HTTP transport honours git's proxy, TLS, speed and `extraHeader`
  settings and `protocol.allow`, sends a git user agent, follows the address
  discovery redirected to, and can fall back to protocol v0 when the server
  lacks the v2 features we asked for.

## Passwords and the secret store

- Credential helpers are no longer started as external programs: the Windows
  Credential Manager (the generic and wincred formats) and Linux's Secret
  Service (the generic and libsecret formats) are read directly; `credential.<url>`
  settings apply with the same URL matching git uses.
- The master password can be changed, a backup password is offered, and
  keyring failures are explained; a broken `config.toml` is recovered
  instead of failing to start. Logs mask token headers, URLs of any scheme,
  keys that look like secrets, and composite values.

## CI

- Oracle tests against system git are split from the unit tests, race tests
  are split the same way with a build cached per suite; test suites run in
  parallel and duplicate or superseded runs are stopped; a build on `main`
  skips suites already run on the same tree on `develop`.
