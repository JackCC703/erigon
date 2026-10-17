#!/usr/bin/env bash
# Reproduce the EIP-7807 payload root, generalized index table, and Merkle
# branches, then cross-check them against remerkleable.
#
#   ./dev/eip7807demo/verify.sh
#
# The only network access is a one-time clone of remerkleable at the pinned
# commit; everything else runs locally.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
verify_dir="$here/verify"
cache_dir="$verify_dir/.cache"
remerkleable_rev="2f0baeef0082d4278acaef7d822deb7009d7db7e"
# Prefer a python whose ensurepip works; homebrew's python3 can point at a
# version whose venv bootstrap is broken.
python_bin="${PYTHON:-}"
if [ -z "$python_bin" ]; then
  for candidate in python3.12 python3.13 python3.11 python3; do
    if command -v "$candidate" >/dev/null 2>&1; then
      python_bin="$candidate"
      break
    fi
  done
fi
if [ -z "$python_bin" ]; then
  echo "no python3 interpreter found; set PYTHON=/path/to/python3" >&2
  exit 1
fi

echo "== go test: schema, generalized indices, proofs"
(cd "$repo" && go test ./dev/eip7807demo/... -count=1)

echo
echo "== demo report"
(cd "$repo" && go run ./dev/eip7807demo > "$verify_dir/report.json")
"$python_bin" -c "
import json
report = json.load(open('$verify_dir/report.json'))
print('payload_root', report['payload_root'])
for proof in report['proofs']:
    print(f\"  {proof['field']:<24} gindex {proof['gindex']:>4}  {proof['branch_length']} nodes\")
"

if [ ! -d "$verify_dir/.venv" ]; then
  echo
  echo "== create python venv"
  "$python_bin" -m venv "$verify_dir/.venv"
  "$verify_dir/.venv/bin/pip" install -q 'rlp==5.0.0'
fi

if [ ! -d "$cache_dir/remerkleable/.git" ]; then
  echo
  echo "== clone remerkleable @ $remerkleable_rev"
  mkdir -p "$cache_dir"
  git clone -q https://github.com/ethereum/remerkleable.git "$cache_dir/remerkleable"
  git -C "$cache_dir/remerkleable" checkout -q "$remerkleable_rev"
fi

echo
echo "== remerkleable cross-check"
(cd "$verify_dir" && PYTHONPATH="$cache_dir/remerkleable" .venv/bin/python verify.py report.json)
