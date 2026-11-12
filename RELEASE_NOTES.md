# Go.Git 1.7.3 — Remembering passphrases without a secret store

The "Remember" checkbox in the SSH key passphrase and repository password dialogs
now works when the secret store does not exist yet.

## Fixes

- An SSH key passphrase entered with "Remember in the Go.Git store" was not saved
  when the secret store (`vault.bin`) had not been created: Go.Git only wrote a
  warning to its log and asked for the passphrase on every connection. The store
  is now created the first time something is remembered: on Windows with a DPAPI
  key and no questions, on Linux with a key from the system keyring or with a
  master password. Cancelling the setup saves nothing.
- The same fix applies to an HTTPS repository password saved with "Remember for
  this resource".
