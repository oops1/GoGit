# Go.Git 1.7.2 — Text as in Windows, collapsible compare and conflict windows

A new engine, text drawn the way Windows is set up, compare and conflict-resolution
windows that collapse their sides and remember how they were left, and main
branches first in the Branches pane.

## Text

- The headless-gui engine is updated to v4.5.0 (from v3.43.0). Text follows the
  system setting: with ClearType on it uses subpixel smoothing (measured the same as
  Windows itself on the same font) with the monitor's subpixel order; in a remote
  session (RDP) and with greyscale smoothing it is grey. Turning ClearType on or off
  or connecting over RDP changes it at once, without a restart.
- The interface font on Windows is Verdana: at small sizes it reads more clearly
  than the former Go Regular. Linux keeps the old font.
- Dialogs and floating panes in their own windows draw text with the same font and
  smoothing as the main window.
- On Windows 11 the window and the dialogs no longer show a second frame arc at the
  rounded corners: the system draws the frame, accent color included.

## Compare and conflicts

- The compare window collapses its left or right side, the border between them can
  be dragged, and the window has maximize and minimize buttons.
- The conflict-resolution window collapses Ours, Base and Theirs, shows the result
  or the sources at full height, and the border between the top and the result can
  be dragged. New: Redo, the All conflicts menu (take ours or theirs in every
  unresolved conflict, mark every conflict unresolved again) and separate scrolling.
- Both windows reopen the way they were closed: size, collapsed sides, border, base
  pane and joint scrolling are kept in `config.toml`.

## Branches

- git-flow develop and master, every server's default branch and the usual main,
  master and develop come first in a Main group with their own icon, and first on
  every server. The pane menu turns the group off, and selecting obsolete branches
  leaves main branches alone. A server default branch such as `release/current` no
  longer appears again under Releases.

## Fixes

- The installer shows the license text again: since 1.6.1 the license dialog was
  empty.
- A failure in a widget or a handler no longer brings the app down: the engine
  catches it, and Go.Git logs it once per place, with secrets redacted.
- The layout of a window closed twice (by the window and by the engine) is saved
  once.
