# Go.Git 1.1.0 — Network and secrets

Go.Git can now clone, fetch, pull and push. As before, it never runs the system
git: the whole protocol stack is written in Go, and the binary needs nothing
installed next to it.

## Talking to remotes

- Git protocol versions 1 and 2 over Smart HTTP/HTTPS, `git://`, SSH and plain
  paths on disk, with pkt-line framing, side-band progress and capability
  negotiation.
- Local repositories are read directly, so cloning from a folder next to yours
  works with no network and no helper programs.
- Real multi-round `have` negotiation instead of a single step, so an
  incremental fetch transfers only what is missing.
- Packfiles are written with delta compression and received as a stream, with
  thin packs completed from the local object database.
- `clone` (full, bare, single branch, by branch, shallow), `fetch`, `pull`
  (fast-forward only), `push`, `prune`, and remote management.
- `FETCH_HEAD` and the `shallow` file are written in git's own format, so the
  system git reads the result without a complaint.
- `--force-with-lease` refuses to overwrite a remote branch that moved since
  you last saw it.

## Credentials and keys

- An encrypted store, `vault.bin`: XChaCha20-Poly1305 content key wrapped by
  key slots — DPAPI on Windows, Secret Service on Linux, an Argon2id master
  password, or a key file. Slots can be added and removed like LUKS.
- Credentials are matched by the longest URL prefix, the way `git credential`
  does it.
- SSH keys come from your key directory, from the store, or from ssh-agent —
  through a socket on Unix and through the named pipe on Windows.
- `known_hosts` is honoured: an unknown host key is shown with its SHA256
  fingerprint and stored only after you accept it; a changed key is refused
  outright.
- Secrets live in byte slices, are wiped right after use and never reach the log.

## In the window

- A clone dialog that checks the address and offers the branches the server
  advertises.
- Pull, Sync and Push on the toolbar and the new Remote menu now do the work,
  showing progress and a log while it runs, cancellable at any point.
- Dialogs for credentials, for confirming a host key and for unlocking the store.
- Settings pages listing stored credentials and SSH keys, with adding, removing
  and setting a master password.
- Optional auto-fetch on a timer, with the distance to the upstream branch shown
  in the repository tree and the status bar.
- New settings: pull strategy, default remote, pruning gone branches on fetch,
  and clone depth.

## Not here yet

Merge, rebase, cherry-pick, revert, reset, worktrees, conflict resolution and
line-level staging. They are next, in that order.

## Downloads

- `gogit-v1.1.0-windows-amd64.zip` — Windows 10/11, no installer required.
- `gogit-v1.1.0-linux-amd64.tar.gz`, `gogit-v1.1.0-linux-arm64.tar.gz` — Linux.

Verify the archives against `SHA256SUMS`.
