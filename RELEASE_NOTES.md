# Go.Git 1.4.0 — Merges and history

Merging, rebasing commit by commit, cherry-pick, revert, reset, tags, rerere,
blame, file history, the reflog, branch comparison, branch switching and a pane
that explains a commit.

## Merging

- Three-way merging on our own engine: diff3 and zdiff3, renames, mode changes,
  modify-against-delete conflicts and a virtual base for histories with more
  than one merge base. The result matches `git merge` byte for byte across 41
  oracle scenarios, state files and reflog entries included.
- Fast-forward, no-fast-forward and squash; `MERGE_HEAD`, `MERGE_MSG`,
  `MERGE_MODE`, `AUTO_MERGE` and `SQUASH_MSG` are written the way git writes
  them, so git can finish a merge we started and the other way round.
- In the app: the Branch menu, "Merge into current" in the branch panel, a
  dialog for the modes, an operation window, a banner for an unfinished merge
  with "Commit…" and "Abort", "take ours/theirs", "mark resolved" and the
  `MERGE_MSG` text waiting in the commit dialog.
- `rerere`: a conflict is remembered under the same id git uses, the resolution
  is recorded when you commit, and it comes back on its own when the same
  conflict appears again; `rerere.autoupdate` stages it for you.

## History

- cherry-pick and revert with `CHERRY_PICK_HEAD`/`REVERT_HEAD`, continue and
  abort, offered in the journal's context menu.
- rebase, `--onto`, and commit by commit: pick, reword, edit, squash, fixup and
  drop. The state in `.git/rebase-merge` is git-compatible — git continues a
  rebase we stopped, and we continue one git stopped. A dialog picks what
  happens to each commit and reorders them.
- `reset` in the soft, mixed and hard modes and for paths, with `ORIG_HEAD` and
  a reflog entry; started from the journal.
- Annotated and lightweight tags: created from the journal, deleted from the
  branch panel.
- blame that follows renames — line for line what `git blame --porcelain` says;
  file history with `--follow`; both open from the files panel.
- A branch's reflog, with the option to put the branch back on any record.
- A comparison of two branches: how many commits are only here and only there,
  and the files that differ.
- A pane for the selected commit: Information (author, hash, dates, parents, the
  branches and tags around it), Changes and Files.
- Journal filters by branch, author and message substring, applied as a
  predicate on the lazy history walk.

## Branches and files

- Branch switching from a dialog: a local branch, or a new one from a remote
  branch that it follows from the start.
- The Compare files window: two sides, editing and saving, navigation between
  changes; the files selected in the working copy are filled in for you.
- Panes torn off into their own windows come back when the layout is reset.
