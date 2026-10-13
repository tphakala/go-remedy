#!/usr/bin/env bash
# Compares the golangci-lint version pinned in .golangci-version with the
# latest golangci-lint release and keeps a single open issue while the pin
# is behind. Dependabot does not read .golangci-version, so this is the only
# signal that the pin has fallen behind. Run by
# .github/workflows/golangci-lint-pin.yml; needs GH_TOKEN and GH_REPO.
set -euo pipefail

pin_file=${PIN_FILE:-.golangci-version}
# The drift issue is recognised by the marker in its body or, if someone
# edited the marker away, by this exact title.
export ISSUE_TITLE='golangci-lint pin is behind the latest release'
export ISSUE_MARKER='<!-- golangci-lint-pin-drift -->'
semver='^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'

# Same normalisation as ci.yml and Taskfile.yml: drop all whitespace.
pin=$(tr -d '[:space:]' < "$pin_file")
# /releases/latest skips drafts and pre-releases. A failed call ends the run
# here (set -e), so an API error fails the job instead of passing it.
latest=$(gh api repos/golangci/golangci-lint/releases/latest --jq .tag_name)

if [[ ! $pin =~ $semver ]]; then
  echo "::error::$pin_file holds '$pin', expected a tag such as v2.14.0" >&2
  exit 1
fi
if [[ ! $latest =~ $semver ]]; then
  echo "::error::latest golangci-lint release tag is '$latest', expected a tag such as v2.14.0" >&2
  exit 1
fi

# is_behind A B succeeds when version A is older than version B. A
# pre-release of B (B-rc.1) counts as older than B.
is_behind() {
  local a=${1%%-*} b=${2%%-*} sorted
  if [[ $a == "$b" ]]; then
    [[ $1 == *-* && $2 != *-* ]]
    return
  fi
  sorted=$(printf '%s\n%s\n' "$a" "$b" | sort -V)
  [[ ${sorted%%$'\n'*} == "$a" ]]
}

# Plain assignment, not a process substitution, so a failed listing stops the
# run before it can conclude that no issue exists and file a duplicate.
open_issues=$(gh issue list --state open --limit 1000 --json number,title,body \
  --jq '[.[] | select(((.body // "") | contains(env.ISSUE_MARKER)) or .title == env.ISSUE_TITLE) | .number] | sort | .[]')
issues=()
if [[ -n $open_issues ]]; then
  mapfile -t issues <<< "$open_issues"
fi

if ! is_behind "$pin" "$latest"; then
  echo "pin $pin is current (latest release $latest)"
  for n in "${issues[@]}"; do
    gh issue close "$n" --reason completed \
      --comment "\`.golangci-version\` is now $pin and the latest golangci-lint release is $latest, so the pin is current."
  done
  exit 0
fi

echo "pin $pin is behind the latest release $latest"
state="<!-- pin=$pin latest=$latest -->"
body="$ISSUE_MARKER
$state
\`.golangci-version\` pins golangci-lint $pin. The latest release is [$latest](https://github.com/golangci/golangci-lint/releases/tag/$latest).

To update:

1. Set \`.golangci-version\` to \`$latest\`.
2. Run \`task tools\` to install that version, then \`task check\`. It runs \`golangci-lint config verify\`, the lint and the ruleguard probe (\`task rules\`), which fails while the installed golangci-lint differs from the pin.
3. Fix any new findings and commit with a \`deps:\` prefix.

The pin must always be a release that supports the Go minor in \`go.mod\`, so a bump of that Go minor means bumping the pin in the same change.

The golangci-lint pin workflow (\`.github/workflows/golangci-lint-pin.yml\`) opened this issue. It rewrites this description when a newer release appears and closes the issue once the pin is current, so add notes as comments."

if (( ${#issues[@]} == 0 )); then
  gh issue create --title "$ISSUE_TITLE" --body "$body"
  exit 0
fi

# Keep the oldest open issue and close any others, so at most one stays open
# even after someone reopens an issue the workflow had already replaced.
n=${issues[0]}
for dup in "${issues[@]:1}"; do
  gh issue close "$dup" --reason "not planned" --comment "Duplicate of #$n."
done
current=$(gh issue view "$n" --json body --jq .body)
if [[ $current == *"$state"* ]]; then
  echo "issue #$n already reports $pin and $latest"
  exit 0
fi
gh issue edit "$n" --body "$body"
gh issue comment "$n" --body "The latest golangci-lint release is now $latest; \`.golangci-version\` still pins $pin."
