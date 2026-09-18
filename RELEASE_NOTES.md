# Go.Git 1.5.1 — Reach

Submodules, corporate HTTP authentication, line history, git's own diff
drivers, and a `commit-graph` reader that makes the journal fast on long
histories.

## The file list sees other clients' commits

- The index is read again when another git client rewrote it. Until now
  `.git/index` was read once, when the repository was opened, so a commit made
  in a console or in another program left stale changes in the file list.

## Submodules

- `.gitmodules` is read with every field it can carry, and relative URLs are
  resolved against `origin` the way git resolves them.
- Init, update, sync, add, remove, unregister and reset — from the menu and
  from the Branches panel, with an add dialog that asks for the URL, the path
  and the branch.
- Cloning offers to bring the submodules along, switching branches creates and
  removes submodule working copies according to `submodule.recurse`, and fetch
  and pull descend into changed submodules according to
  `fetch.recurseSubmodules`.
- The Branches panel shows submodules and their state, and working-copy status
  honours `submodule.<name>.ignore` — and can report a submodule in spite of
  it when asked to.

## NTLM and Kerberos authentication

- NTLMSSP in pure Go and SPNEGO for the Negotiate scheme: through SSPI on
  Windows, through a Kerberos ticket (gokrb5) on Linux. The password comes
  from the secret store and reaches neither the log nor a process argument.
- Both schemes work through proxies, including the CONNECT tunnel, and bind to
  the server certificate (`tls-server-end-point`). The operation log names the
  scheme it picked.

## Investigate — line history

- `git log -L` in full: the history of the selected lines from the file list,
  from the diff pane and from blame, with a window listing the commits that
  changed them and the selection carried onto the revision you pick.

## Diff drivers

- Git's built-in `userdiff` drivers for a couple of dozen languages: hunk
  headers come from the driver a path is given in `.gitattributes`, together
  with `diff.<driver>.funcname` and `xfuncname` from the configuration. POSIX
  regular expressions are translated byte for byte, so they match git on
  content that is not UTF-8.
- The `-L :funcname:file` range rests on the path's driver as well.

## commit-graph

- The `commit-graph` file and split chains are read in full: corrected commit
  dates, generation numbers and changed-path (bloom) filters.
- The revision walk, line history, path history, merge-base and the
  ahead/behind count go through the graph and skip commits its path filters
  rule out. A missing or rewritten graph is detected and left unused.

## Also

- Rename detection is limited to the destination paths asked for, so path
  history no longer looks for renames it would not show anyway.
- A depth-limited fetch sends the history beyond the client's shallow
  boundary, and the `shallow` file is written sorted the way git writes it.
