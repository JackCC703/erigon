# eip7807demo

Local demo for the EIP-7807 SSZ execution block work: it prints the
`ExecutionPayload` root of a deterministic fixture, the generalized index table
for all 18 top-level fields, and Merkle branches for selected fields. It runs
without a node, database, or network access.

## One-command reproduction

```sh
./dev/eip7807demo/verify.sh
```

Prints the pinned payload root, runs the Go tests, and cross-checks the root,
the generalized indices, and the branches against remerkleable.

## Commands

```sh
go run ./dev/eip7807demo                          # full JSON report
go run ./dev/eip7807demo -gindex                  # generalized index table only
go run ./dev/eip7807demo -fields parent_hash,withdrawals
go test ./dev/eip7807demo/... -count=1
```

`-fields` accepts any top-level field name and defaults to `parent_hash`,
`transactions`, `receipts_root`, and `withdrawals`. The command exits non-zero
if the payload root does not match the pinned value or if any branch fails to
fold back into the root.

## Pinned values

Derived from EIP-7807 as of blob `3f2dc991ac65153842fd9b894c04ba590d58d3e6`,
which links consensus-specs revision `4f1df00ff567ea1855fb7f37ec23bd324a96703b`.

| Value | Hex |
| --- | --- |
| `ExecutionPayload` root | `0x51951516710af90e8c20092e086f37232c69b558d6ba0035333982e115e1f627` |
| `requests_hash` | `0xb1c4c413201f39ef0e4528edbd42e595a5258578a22e8634a9dee79684fdf245` |

## Generalized indices

A progressive container (EIP-7495) does not lay its fields out as an ordinary
container does. Its fields live in a 0-terminated chain of subtrees holding 1,
4, 16, 64, ... chunks (EIP-7916), and `mix_in_active_fields` prepends one level
that hashes the field root with the packed `active_fields` bitvector. Two
consequences drive the table:

- A field's index never moves when fields are appended — that is the point of
  the type — so a branch stays valid across payload versions.
- The mix-in level adds a leading path bit, which is why the mixed index is not
  simply twice the unmixed one.

`gindex_no_mix_in` is the index inside the progressive tree alone; `gindex` is
the index a verifier uses against the payload root.

| # | Field | `gindex_no_mix_in` | `gindex` |
| - | - | - | - |
| 0 | `parent_hash` | 2 | 4 |
| 1 | `miner` | 24 | 40 |
| 2 | `state_root` | 25 | 41 |
| 3 | `transactions` | 26 | 42 |
| 4 | `receipts_root` | 27 | 43 |
| 5 | `number` | 224 | 352 |
| 6 | `gas_limits` | 225 | 353 |
| 7 | `gas_used` | 226 | 354 |
| 8 | `timestamp` | 227 | 355 |
| 9 | `extra_data` | 228 | 356 |
| 10 | `mix_hash` | 229 | 357 |
| 11 | `base_fees_per_gas` | 230 | 358 |
| 12 | `withdrawals` | 231 | 359 |
| 13 | `excess_gas` | 232 | 360 |
| 14 | `parent_beacon_block_root` | 233 | 361 |
| 15 | `requests_hash` | 234 | 362 |
| 16 | `block_access_list` | 235 | 363 |
| 17 | `slot_number` | 236 | 364 |

## What is checked, and by what

| Check | Independent source |
| --- | --- |
| Payload root | `payload.HashSSZ()` in `execution/types/sszblocks`, pinned in `payload_test.go` |
| Payload root, recomputed | remerkleable `ExecutionPayload.hash_tree_root()`, rebuilt from raw field values |
| Field roots | `FieldRoots(payload)`, exercised field by field against `HashSSZ` |
| Generalized indices | remerkleable `ProgressiveContainer.chunk_to_gindex()` |
| Merkle branches | folded with remerkleable's own field roots and indices back to its own root |

`verify.sh` shells out to remerkleable only; no Go-produced root is reused as an
input to the Python side.

## Layout

| Path | Purpose |
| --- | --- |
| `main.go` | command entry point, JSON report, branch verification |
| `internal/eip7807/schema.go` | field schema, generalized indices, field roots, proofs |
| `internal/report/report.go` | report rendering and pinned constants |
| `verify/verify.py` | remerkleable cross-check |
| `verify.sh` | one-command end-to-end run |

`internal/report` deliberately keeps the pinned constants in Go, not Python, so
the Python side never supplies the expected answer.

## Regenerating artifacts

`verify.sh` writes `verify/report.json` and creates `verify/.venv` plus
`verify/.cache/`, all git-ignored.
