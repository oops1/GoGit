#!/usr/bin/env bash
set -euo pipefail

version="$1"
prefix="$HOME/gits/$version"

# fetch saves a download to a file, so a retry starts the file afresh
# instead of feeding a second copy into a pipe that already holds part
# of the first. github.com answers 503 and 504 now and then, and one such
# answer used to fail the whole job.
fetch() {
  curl -sSfL --connect-timeout 30 --retry 5 --retry-delay 10 --retry-all-errors \
    -o "$2" "$1"
}

install_linux() {
  if [ ! -x "$prefix/bin/git" ]; then
    {
      sudo apt-get update -qq
      sudo apt-get install -y --no-install-recommends \
        libcurl4-openssl-dev zlib1g-dev libexpat1-dev libssl-dev
      work=$(mktemp -d)
      archive="$work/git-$version.tar.gz"
      # kernel.org carries the release tarballs too, and they unpack to the
      # same git-$version directory, so it stands in when github.com is down.
      fetch "https://github.com/git/git/archive/refs/tags/v$version.tar.gz" "$archive" ||
        fetch "https://mirrors.edge.kernel.org/pub/software/scm/git/git-$version.tar.gz" "$archive"
      tar xzf "$archive" -C "$work"
      make -C "$work/git-$version" -j"$(nproc)" prefix="$prefix" NO_TCLTK=1 NO_GETTEXT=1 install
      rm -rf "$work"
    } >&2
  fi
  if [ ! -x "$prefix/bin/git" ]; then
    echo "::error::git $version did not build" >&2
    exit 1
  fi
  echo "$prefix/bin"
}

install_windows() {
  if [ ! -x "$prefix/cmd/git.exe" ]; then
    {
      mkdir -p "$prefix"
      fetch "https://github.com/git-for-windows/git/releases/download/v$version.windows.1/PortableGit-$version-64-bit.7z.exe" \
        "$HOME/portable-git.exe"
      "$HOME/portable-git.exe" -y "-o$(cygpath -w "$prefix")"
      rm -f "$HOME/portable-git.exe"
    } >&2
  fi
  if [ ! -x "$prefix/cmd/git.exe" ]; then
    echo "::error::PortableGit $version left no git.exe in $prefix/cmd" >&2
    exit 1
  fi
  echo "$prefix/cmd"
}

case "${RUNNER_OS:-}" in
  Linux)
    install_linux
    ;;
  Windows)
    install_windows
    ;;
  *)
    echo "::error::install-git.sh does not know how to serve ${RUNNER_OS:-an unnamed system}" >&2
    exit 2
    ;;
esac
