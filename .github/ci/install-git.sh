#!/usr/bin/env bash
set -euo pipefail

version="$1"
prefix="$HOME/gits/$version"

install_linux() {
  if [ ! -x "$prefix/bin/git" ]; then
    sudo apt-get update -qq
    sudo apt-get install -y --no-install-recommends \
      libcurl4-openssl-dev zlib1g-dev libexpat1-dev libssl-dev
    work=$(mktemp -d)
    curl -sSfL "https://github.com/git/git/archive/refs/tags/v$version.tar.gz" | tar xz -C "$work"
    make -C "$work/git-$version" -j"$(nproc)" prefix="$prefix" NO_TCLTK=1 NO_GETTEXT=1 install
    rm -rf "$work"
  fi
  if [ ! -x "$prefix/bin/git" ]; then
    echo "::error::git $version did not build"
    exit 1
  fi
  echo "$prefix/bin" >> "$GITHUB_PATH"
}

install_windows() {
  if [ ! -x "$prefix/cmd/git.exe" ]; then
    mkdir -p "$prefix"
    curl -sSfL -o "$HOME/portable-git.exe" \
      "https://github.com/git-for-windows/git/releases/download/v$version.windows.1/PortableGit-$version-64-bit.7z.exe"
    "$HOME/portable-git.exe" -y "-o$(cygpath -w "$prefix")"
    rm -f "$HOME/portable-git.exe"
  fi
  if [ ! -x "$prefix/cmd/git.exe" ]; then
    echo "::error::PortableGit $version left no git.exe in $prefix/cmd"
    exit 1
  fi
  cygpath -w "$prefix/cmd" >> "$GITHUB_PATH"
}

case "${RUNNER_OS:-}" in
  Linux)
    install_linux
    ;;
  Windows)
    install_windows
    ;;
  *)
    echo "::error::install-git.sh does not know how to serve ${RUNNER_OS:-an unnamed system}"
    exit 2
    ;;
esac
