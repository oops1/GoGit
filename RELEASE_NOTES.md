# Go.Git 1.6.2 — Errors that stay

A failed operation is no longer lost, and a network failure while finishing a
Git-Flow branch is named and comes with a way out.

## Failed operations are written to the log

- The error of any operation — fetch, push, Git-Flow, merge and the rest — used
  to be visible only in the operation window and disappeared with it. It is now
  written to `gogit.log` as `operation failed` with the operation's name and the
  error text (passwords in addresses are cut out). A cancelled operation is not
  counted as a failure.

## A network failure while finishing Git-Flow

- Finishing a feature, release or hotfix starts by fetching from the server. If
  that fetch failed, the operation used to just end with an error, and it was
  not clear that nothing in the repository had changed. The operation window
  now says exactly that and then offers to finish without fetching — with a
  warning that the push after finishing may be rejected if the server has newer
  commits. The other settings of the finish dialog are kept for the retry.
