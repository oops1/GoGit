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

## Appearance

- Settings gained a theme colour editor: accent, window background, fields,
  text and secondary text are typed as `#RRGGBB`, with a swatch next to each
  field and a preview of them together. An empty field keeps the colour of the
  system theme, and one button clears all five. The colours live in
  `config.toml`.
- Drop-down lists draw a chevron instead of a triangle behind a divider.
- A pane can be torn off into a separate operating-system window: engine 3.23
  carries it and our markup switches it on.
