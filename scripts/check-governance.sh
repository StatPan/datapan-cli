#!/bin/sh
set -eu

required_files='LICENSE NOTICE CONTRIBUTING.md SECURITY.md TRADEMARKS.md docs/source-rights.md'
for file in $required_files; do
  if [ ! -f "$file" ]; then
    echo "missing required governance file: $file" >&2
    exit 1
  fi
done

for file in $required_files; do
  if ! git ls-files --error-unmatch "$file" >/dev/null 2>&1; then
    echo "required governance file is not tracked: $file" >&2
    exit 1
  fi
done

for link in 'NOTICE' 'CONTRIBUTING.md' 'SECURITY.md' 'TRADEMARKS.md' 'docs/source-rights.md'; do
  if ! grep -F "($link)" README.md >/dev/null; then
    echo "README.md is missing governance link: $link" >&2
    exit 1
  fi
done

if ! grep -F 'https://github.com/StatPan/datapan-cli/security/advisories/new' SECURITY.md >/dev/null; then
  echo 'SECURITY.md is missing the private GitHub vulnerability-reporting route' >&2
  exit 1
fi

if ! grep -F 'Apache License' NOTICE >/dev/null || ! grep -F 'provider' NOTICE >/dev/null; then
  echo 'NOTICE does not state the Apache/provider-rights boundary' >&2
  exit 1
fi

if ! grep -F 'does not grant permission to use the Datapan name' TRADEMARKS.md >/dev/null; then
  echo 'TRADEMARKS.md does not state the branding boundary' >&2
  exit 1
fi

echo 'governance check passed'
