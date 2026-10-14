# Go.Git 1.6.4 — Help in the console

The command console now has a reference with examples, and the ahead and behind
counter of a branch no longer dims its name.

## Help in the command console

- The `help` command prints how the console works and lists every command with
  a short description; `help <command>` shows the options and examples of one
  command (`help log`, `help push`). The new Help button in the window does the
  same in one click. The texts come in Russian and English, and the reference
  works even when no repository is open. A test checks every example: the
  command parses, and every option in it is one the command accepts.

## The branch counter

- The `↑3 ↓1` counter of a branch in the Branches pane, and of the active
  repository in the Repositories pane, is now drawn as a separate muted piece of
  the row instead of being appended to the label. Before, the whole name of a
  diverged branch was dimmed. The headless-gui engine is updated to v3.35.1 for
  this; it also keeps popup menus inside the window.
