# Go.Git 1.5.3 — Every address

Push now reaches every address of a remote, the theme takes your own colours,
and a pane can be torn off into a window of its own.

## Push to every address

- A remote may carry several `pushurl` entries, and git sends the push to all
  of them. We used only the first one, so a mirror silently fell behind. Every
  address is now pushed to in turn, a failure of one does not cancel the rest,
  and the operation log names each address and what went there. Checked
  against git with two `pushurl` entries.
- An address is shown without its password, in the log and in the console.
- `remote -v` prints every push address on its own line, as git does. `remote
  set-url --push` learned `--add` and `--delete`: until now any edit replaced
  all the addresses with one.
- The Remotes window shows every push address and lets you edit them as a
  comma-separated list; an empty field means the push goes to the fetch URL.

## Blame of a large file no longer freezes the window

- Blame used to push gigabytes through memory, and the garbage collector
  stopped the whole window while it did: every step of the history split the
  file into lines again and built the line table for both sides again. Each
  revision is now split once, the line table and the working buffers live for
  the whole pass, and a delta target gets its room up front. On a 30 000 line
  file (`swagger.json`, 1 MB) a single blame allocated 2.4 GB and now allocates
  1.0 GB; the internal benchmarks went from 3.0 GB to 0.72 GB and from 1.5 GB
  to 0.61 GB. The blame result is checked against git and did not change.

## Long lines and colours: engine 3.25

- The engine moved to 3.25. The comparison and conflict windows gained a
  horizontal scrollbar, so a long line can be read to its end; Shift+wheel and
  the caret move the code sideways too. This was the last task left in the
  release, and no workaround existed on our side.
- The theme colour editor in Settings now uses a real colour picker: a field
  with a swatch, a palette and the `#RRGGBB` code.

## What keeps the window busy

- When another operation is already running, the app no longer answers with a
  quiet "Busy": it names the operation ("Busy: Push") and offers to stop it.
  Until now, pressing "Switch branch" while an operation hung looked like the
  program simply ignoring the click.
- The busy flag and the repository watcher's pause are released on every exit
  from an operation, not only on a normal return.
- When the process passes a memory threshold (512 MiB, then each doubling),
  `gogit.log` records the heap size, the object count, the open repository and
  the running operation, so growth is visible in the log.
- `GOGIT_PPROF=127.0.0.1:6060` turns on Go's profiler on the loopback
  interface. It is off by default and refuses an address outside the loopback.

## Appearance

- Settings gained a theme colour editor: accent, window background, fields,
  text and secondary text are typed as `#RRGGBB`, with a swatch next to each
  field and a preview of them together. An empty field keeps the colour of the
  system theme, and one button clears all five. The colours live in
  `config.toml`.
- Drop-down lists draw a chevron instead of a triangle behind a divider.
- The icon of a remote's default branch (`origin/master`) no longer repeats the
  icon of the current branch, which read as if the app stood on `master` while
  the current branch was another one.
- A pane can be torn off into a separate operating-system window: engine 3.23
  carries it and our markup switches it on.
