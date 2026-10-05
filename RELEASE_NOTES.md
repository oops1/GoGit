# Go.Git 1.6.3 — Every address gets everything

Pushing to several addresses no longer loses objects, and the commit at the
edge of a shallow clone opens.

## Pushing to several addresses

- For a remote with several `pushurl` entries, a push to the second and later
  addresses could send too little. To decide what the server already had, we
  looked not only at the references it advertised itself but also at the local
  remote-tracking branches `origin/*` — and those reflect only the first
  address. When a commit was on the first server and not on the second, the
  second received a lone tag without its commit and trees, and answered
  `unpack error: unpack-objects abnormal exit` or `missing necessary objects`.
  Each address is now judged only by what that very server advertised, as git
  does. Checked against git 2.30.2, 2.39.2 and 2.55.0 with a real git daemon:
  after the push both repositories pass `git fsck`.

## Shallow clone

- In a depth-1 clone the commit at the edge of the history would not open: to
  show its files we read its parent, which the clone does not have, so the
  status line said `odb: object not found` and the Commit pane stayed empty.
  Such a commit is now treated as a root, as git does, and all of its files are
  shown as added.
