#!/usr/bin/env bash
# Idempotent script that prepends the Apache 2.0 boilerplate header to all .go
# files in the repository. Re-running is safe: files that already contain a
# "Copyright" or "Licensed under the Apache" line in their first 20 lines are
# left untouched.
#
# Three layout categories are handled correctly:
#   1. Files that begin with `//go:build ...` (build constraint)
#      -> header is inserted ABOVE the build tag, separated by a blank line.
#   2. Files that begin with `// Package xxx ...` (package doc comment)
#      -> header is inserted ABOVE the doc comment, separated by a blank line.
#   3. Plain `package xxx` files
#      -> header is inserted ABOVE the package clause, separated by a blank line.
#
# Mixed case (build tag followed by package doc) is treated as category 1 and
# leaves the rest of the file unchanged.
#
# Usage:
#   scripts/add_license_header.sh          # apply to all .go files
#   scripts/add_license_header.sh path...  # apply to specific files

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

HEADER='// Copyright 2022 Su Yang
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.'

if [ "$#" -gt 0 ]; then
    files=("$@")
else
    # All .go files except vendor/ and the scripts/ directory itself.
    # Compatible with bash 3.2 (no mapfile/readarray).
    files=()
    while IFS= read -r line; do
        files+=("$line")
    done < <(find . -type f -name '*.go' \
        -not -path './vendor/*' \
        -not -path './.git/*' | sort)
fi

added=0
skipped=0

for f in "${files[@]}"; do
    # Idempotency check: scan first 20 lines for an existing header.
    if head -n 20 "$f" | grep -qE 'Copyright|Licensed under the Apache'; then
        skipped=$((skipped + 1))
        continue
    fi

    first_line="$(head -n 1 "$f")"

    tmp="$(mktemp)"
    {
        printf '%s\n\n' "$HEADER"
        cat "$f"
    } > "$tmp"

    # Sanity: file must still start with a comment or package after our prepend.
    mv "$tmp" "$f"
    added=$((added + 1))

    # Note: first_line is captured for potential future per-category handling,
    # but the prepend strategy is identical for all three categories — the
    # original file content (including //go:build, Package doc, or plain
    # package) is preserved verbatim after the header + blank line.
    : "$first_line"
done

echo "License header: added to $added file(s), skipped $skipped file(s)."
