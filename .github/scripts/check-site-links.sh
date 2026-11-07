#!/usr/bin/env bash
# Fails when a page in site/ links to a local file that does not exist.
set -euo pipefail
cd "$(dirname "$0")/../../site"
rc=0
while IFS= read -r f; do
  dir=$(dirname "$f")
  while IFS= read -r link; do
    link=${link%%#*}
    [[ -z $link || $link == *:* ]] && continue
    target="$dir/$link"
    [[ -d $target ]] && target="$target/index.html"
    [[ -e $target ]] || { echo "$f: broken link $link"; rc=1; }
  done < <(grep -oE '(href|src)="[^"]*"' "$f" | sed -E 's/^(href|src)="//; s/"$//')
done < <(find . -name '*.html')
[[ $rc == 0 ]] && echo "all local links resolve"
exit $rc
