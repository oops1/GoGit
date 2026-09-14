#!/usr/bin/env bash
set -uo pipefail

suite="$1"

annotate_race() {
  sed -n '/WARNING: DATA RACE/,/^==================$/p' "$1" | head -60 |
    while IFS= read -r line; do echo "::error::$line"; done
  grep -E "FAIL|panic:|_test\.go:" "$1" | head -40 |
    while IFS= read -r line; do echo "::error::$line"; done
}

annotate_oracle() {
  grep -E "^(--- )?FAIL|^ +--- FAIL|panic:" "$1" | head -3 |
    while IFS= read -r line; do echo "::error::${line:0:400}"; done
  grep -E "_test\.go:[0-9]+:" "$1" | head -5 |
    while IFS= read -r line; do echo "::error::${line:0:1200}"; done
  {
    echo "### $suite failures on $RUNNER_OS"
    echo '```'
    awk '/^--- FAIL/ { seen = 1 } seen' "$1" | head -200
    echo '```'
  } >> "$GITHUB_STEP_SUMMARY"
}

case "$suite" in
  race)
    go vet -unsafeptr=false ./... || exit 1
    CGO_ENABLED=1 go test -count=1 -timeout=40m -race ./... 2>&1 | tee race.log
    status=${PIPESTATUS[0]}
    if [ "$status" -ne 0 ]; then
      annotate_race race.log
    fi
    exit "$status"
    ;;
  oracle-ops)
    packages="./internal/gitcore/ops/"
    ;;
  oracle-app)
    packages="./internal/app/"
    ;;
  oracle-rest)
    packages=$(go list ./... | grep -v -e '/internal/gitcore/ops$' -e '/internal/app$')
    ;;
  *)
    echo "::error::unknown suite $suite"
    exit 2
    ;;
esac

go test -count=1 -timeout=40m -tags oracle -coverprofile=cover.out -covermode=atomic $packages 2>&1 | tee oracle.log
status=${PIPESTATUS[0]}
if [ "$status" -ne 0 ]; then
  annotate_oracle oracle.log
  exit "$status"
fi
total=$(go tool cover -func=cover.out | tail -1 | awk '{print $3}' | tr -d '%')
echo "$suite coverage: $total%"
awk -v t="$total" 'BEGIN { if (t + 0 < 90) { print "coverage below 90%"; exit 1 } }'
