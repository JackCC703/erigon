# Erigon SSZ Execution Blocks and EIP-7807

**EPF Cohort 7 — Final Report**

Author: Chen Can (Jack CC, [@JackCC703](https://github.com/JackCC703)) · Mentors: RazorClient, Etan Kissling

- Proposal: [erigon-ssz-execution-blocks-eip-7807.md](https://github.com/eth-protocol-fellows/cohort-seven/blob/main/projects/erigon-ssz-execution-blocks-eip-7807.md)
- Final report (HackMD): https://hackmd.io/@jackcc/epf7-finalreport
- Design document: https://hackmd.io/@jackcc/epf7-project-draft
- EIP: [EIP-7807 — SSZ execution blocks](https://eips.ethereum.org/EIPS/eip-7807) (author: Etan Kissling)

## Abstract

This project built the Erigon-side foundations for EIP-7807, which proposes replacing the RLP-encoded execution block with an SSZ representation. Work followed the proposal in four phases: field mapping, progressive SSZ Merkleization primitives, a payload adapter that computes the EIP-7807 block root from existing Erigon block data, and — covered by this report — generalized index mappings, Merkle proof generation, and a reproducible local demonstration.

The payload adapter produces a root that agrees with an independent remerkleable implementation. Three progressive SSZ components were merged into Erigon. The EIP-7807 adapter PR was approved and then closed when the EIP was declined for inclusion. The proof and demonstration work is published on a public branch of the author's Erigon fork as a local command-line tool.

## 1. Context

Ethereum uses two different commitment schemes across its layers. The Consensus Layer commits to SSZ `hash_tree_root`; execution blocks are committed through RLP and Merkle-Patricia trie roots. EIP-7807 removes that split by giving the execution block an SSZ layout, with three consequences worth building toward:

1. **A normalized block hash.** The Consensus Layer can verify a builder's execution block commitment synchronously instead of waiting on the Execution Layer.
2. **Proving support.** Individual block fields become provable without the full block. With `ProgressiveContainer` (EIP-7495) and `ProgressiveList` (EIP-7916), a proof stays valid as the container grows, so verifiers do not need redeployment every fork.
3. **Better transport.** Fully SSZ data allows a binary Engine API, cutting exchanged payload size substantially.

Erigon was the target because it already carries real SSZ infrastructure in Caplin and `cl/merkle_tree`, while its canonical execution block model remains RLP/MPT-oriented. The missing piece was an explicit, tested mapping from Erigon execution data to the EIP-7807 layout, plus the progressive Merkleization, root computation, generalized indices, and proof generation needed to inspect and verify that layout.

The project builds a parallel EIP-7807 root and proof surface next to Erigon's existing block hash and import path.

## 2. Deliverables and status

| Phase | Deliverable | Status |
| --- | --- | --- |
| 1 | EIP-7807 → Erigon field mapping and adapter boundary | Complete |
| 2 | Progressive SSZ primitives required by the layout | Complete, 3 PRs merged |
| 3 | Payload adapter and EIP-7807 block root | Complete, PR closed on protocol decision after approval and green CI |
| 4 | Generalized indices, proof generation, reproducible demonstration | Complete, published on a public fork branch |
| 5 | Final report and presentation | This document |

### Merged upstream

| PR | Title | Merged |
| --- | --- | --- |
| [#22446](https://github.com/erigontech/erigon/pull/22446) | `cl/merkle_tree`: add EIP-7916 progressive merkleization | 2026-07-16 |
| [#22689](https://github.com/erigontech/erigon/pull/22689) | `cl/merkle_tree`: add EIP-7916 progressive list root | 2026-07-27 |
| [#22783](https://github.com/erigontech/erigon/pull/22783) | `cl/merkle_tree`: add progressive byte list root | 2026-08-29 |

Two further PRs ([#22528](https://github.com/erigontech/erigon/pull/22528), [#23130](https://github.com/erigontech/erigon/pull/23130)) covered the EIP-7495 active-fields mix-in and `ProgressiveContainer` root. Both were superseded by [#22912](https://github.com/erigontech/erigon/pull/22912) (`cl: support Gloas alpha.12 progressive SSZ and spectests`), which landed the same functionality along with the spec tests, so they were closed rather than merged. The outcome is the same code in `cl/merkle_tree`; only the review path changed.

### Published outside upstream

| Work | Location |
| --- | --- |
| EIP-7807 payload adapter and root | [#23854](https://github.com/erigontech/erigon/pull/23854), closed |
| Generalized indices, proofs, demo | branch `eip7807-demo` of the [author's fork](https://github.com/JackCC703/erigon/tree/eip7807-demo) |

## 3. Phase 1 — Field mapping

The first step was an explicit adapter boundary. `types.BlockWithReceipts` was chosen as the input because it aggregates the block, receipts, execution requests, and block access list needed for root computation. The mapping covers all 18 top-level fields of the EIP-7807 `ExecutionPayload`, including the three nested containers (two-field `GasAmounts` in three places, two-field `BlobFeesPerGas`).

Canonical element encodings are preserved where the EIP requires them: transactions and receipts keep their EIP-2718 typed-envelope bytes, withdrawals and the block access list keep their RLP representation, and `requests_hash` is aligned with `ExecutionRequests.hash_tree_root()`.

The mapping work surfaced one field with no direct Erigon source. The EIP's `excess_gas.regular` has no equivalent in the legacy header, so it enters through an explicit `BlockAdapterConfig.RegularExcessGas` field rather than being inferred — an input the adapter cannot guess and therefore must be told.

## 4. Phase 2 — Progressive SSZ primitives

EIP-7807 depends on two new Merkle shapes, and neither existed in Erigon.

**EIP-7916 progressive Merkle tree.** Rather than a fixed-capacity balanced tree, chunks live in a 0-terminated chain of subtrees holding 1, 4, 16, 64, … chunks. A chunk is a leaf of the first subtree with room for it, so its position never changes as more chunks arrive. PRs #22446, #22689, and #22783 landed `MerkleizeProgressive`, `ProgressiveListRoot`, and `ProgressiveByteListRoot`.

**EIP-7495 progressive container.** A container over that tree, whose root additionally mixes in the packed `active_fields` bitvector: `mix_in_active_fields(merkleize_progressive(field_roots), active_fields) = hash(root, pack_bits(active_fields))`. Inactive positions contribute zero chunks, which is what allows a field to be dropped without moving its neighbours. This arrived via #22912.

The progressive tree is why an EIP-7807 payload can gain a field without invalidating proofs for existing ones — the property the rest of this project exercises.

## 5. Phase 3 — Payload adapter and block root

`execution/types/sszblocks/payload.go` implements the adapter:

```go
func ExecutionPayloadFromBlock(input *types.BlockWithReceipts, cfg BlockAdapterConfig) (*ExecutionPayload, error)
func (p *ExecutionPayload) HashSSZ() ([32]byte, error)
```

`HashSSZ` builds the 18 field roots and calls `ProgressiveContainerRootAll`, which mixes in the all-active `active_fields` bitvector. The adapter validates its inputs rather than assuming them: nil block or config, a block number that does not fit `uint64`, a missing base fee, missing blob gas, oversized `extra_data`, and missing execution requests are all rejected with distinct errors.

The root was validated two ways: against a deterministic fixture pinned in the test suite, and against an independent remerkleable implementation that rebuilds the payload from original field values rather than from Go-produced roots.

An early discrepancy turned out to be instructive. The first adapter version used Erigon's five-field progressive *Gloas* requests schema for `requests_hash`. Independent Python validation reproduced that root (`0x012a1eff…`) and showed that EIP-7807 instead references the Electra `ExecutionRequests` container with three bounded lists. The adapter was corrected to decode at `ElectraVersion`; the Go and Python roots then agreed on `0xb1c4c413…`. That case is retained in the Python verifier as documentation of the superseded shape.

The final fixture root is:

```
0x51951516710af90e8c20092e086f37232c69b558d6ba0035333982e115e1f627
```

PR #23854 carried the adapter and root with this pinned fixture. It was reviewed and approved with green CI, then closed when EIP-7807 was declined for inclusion at ACDE #245.

## 6. Phase 4 — Generalized indices, proofs, and demonstration

Phase 4 is the part of the project that makes the layout *usable* rather than merely computable: a verifier who receives a field and a branch must be able to rebuild the root without the rest of the block.

### 6.1 Deriving the generalized index table

A progressive container does not lay its fields out like an ordinary container. Two properties decide the table:

- Fields occupy the progressive tree's chains of capacity 1, 4, 16, 64, … chunks rather than a single balanced tree, so the index of a field jumps at each subtree boundary (2, then 24, then 224, then 236).
- The active-fields mix-in prepends one level, so a field's index gains a leading path bit rather than doubling: `gindex = no_mix_in + 2^floor(log2(no_mix_in))`.

This was not obvious from first principles, and an initial implementation that assumed uniform `2^depth | i` layout failed immediately — its table predicted a tree depth of 5 where the library's own proof returned 2 nodes. The contradiction forced a check against the specification, and the resulting derivation was then confirmed against remerkleable's own `to_gindex_progressive` / `chunk_to_gindex`, which encode exactly the same chain-of-subtrees walk.

Both columns are reported, because both are needed: `gindex_no_mix_in` locates the field inside the progressive tree, while `gindex` is what a verifier uses against the payload root. The full table is in the appendix.

### 6.2 Proof generation

`FieldProof(payload, fieldIndex)` returns the Merkle branch for any top-level field plus the field's generalized index, and `VerifyProof(fieldRoot, gindex, branch, want)` folds them back to the root. Branches are ordered leaf-to-root and include the active-fields mix-in level, so a branch's length equals the depth of the field's index.

Branches were generated and verified in Go for `parent_hash`, `transactions`, `receipts_root`, and `withdrawals`:

| Field | `gindex` | Branch nodes |
| --- | --- | --- |
| `parent_hash` | 4 | 2 |
| `transactions` | 42 | 5 |
| `receipts_root` | 43 | 5 |
| `withdrawals` | 359 | 8 |

`FieldProof` accepts any field, not only these four; the CLI's `-fields` flag selects which to print.

### 6.3 A real bug, found only by cross-checking

The first `FieldRoots` implementation hashed `extra_data` as `BytesRoot`, without mixing in its length. `extra_data` is a `ByteList[32]`, so its root must be `hash(merkleize(packed_bytes), len)`.

What makes this worth reporting is how it failed. The mistake was invisible to two checks:

- The branch verification passed. `PayloadRoot` and `VerifyProof` both used the same wrong field root, so folding the branch reproduced the wrong root consistently.
- Every proof was internally valid.

It only surfaced when the field-by-field roots were compared against the library's own `HashSSZ`, where `extra_data` was the single field of 18 that disagreed. A system that agrees with itself is not thereby correct; the independent path is what caught it. That lesson is the main methodological takeaway of this project, and the case is now pinned by a regression test.

### 6.4 Demonstration

`dev/eip7807demo` is a local command that needs no node, database, or network access. A single script reproduces the entire chain from a clean checkout:

```sh
./dev/eip7807demo/verify.sh
```

It runs the Go test suite, prints the report, then cross-checks the root, all 18 indices, and every branch against remerkleable. The command exits non-zero if the root drifts from the pinned value or if any branch fails to fold back, so a broken proof fails the run instead of shipping a bad table.

```sh
go run ./dev/eip7807demo              # full JSON report
go run ./dev/eip7807demo -gindex      # index table only
go run ./dev/eip7807demo -fields parent_hash,withdrawals
```

The JSON report carries the schema revision, the payload root, `requests_hash`, one entry per field (index, type, both indices, value, root), and one entry per proof (branch and length).

## 7. Verification methodology

The point of phase 4 is assurance, so the checks are separated by who produces the expected value. The Python side never receives a Go-computed root as an expectation.

| Check | Independent source of truth |
| --- | --- |
| Payload root | `HashSSZ` in the adapter, pinned in its test suite |
| Payload root, recomputed | remerkleable `hash_tree_root()`, rebuilt from raw field values only |
| Field roots | compared field by field against `HashSSZ`, which caught the `extra_data` bug |
| 18 generalized indices | remerkleable `ProgressiveContainer.chunk_to_gindex()` |
| Merkle branches | folded with remerkleable's own field roots and indices back to remerkleable's own root |
| Index stability | fields appended to 22; the original 18 indices must not move |

Pinned references: EIP-7807 at blob `3f2dc991ac65153842fd9b894c04ba590d58d3e6`, which links consensus-specs revision `4f1df00ff567ea1855fb7f37ec23bd324a96703b`; remerkleable at commit `2f0baeef0082d4278acaef7d822deb7009d7db7e`.

Go-side coverage is 12 tests across the two packages: generalized indices for all 18 fields, index stability under growth, the mix-in arithmetic, the `extra_data` length regression, proofs folding back to the root for every field, out-of-range indices, tampered branches, and truncated branches.

## 8. Reproducibility

The work is published on the `eip7807-demo` branch of the author's fork:

```sh
git clone --branch eip7807-demo https://github.com/JackCC703/erigon.git erigon-demo
cd erigon-demo
./dev/eip7807demo/verify.sh
```

`verify.sh` runs the Go tests, generates the report, builds the Python venv, and clones remerkleable at the pinned commit on first run. It requires Go, Python 3.11+, and network access for that one clone.

The demo uses Erigon's real adapter over a real `BlockWithReceipts`, so the root it prints comes from the same code path that was reviewed in #23854.

## 9. Next steps

1. If EIP-7807 or an equivalent SSZ block commitment returns to scope, the adapter and the index table are directly reusable; the work to redo is the encoding path, not the Merkleization analysis.
2. The generalized index table is the missing input for any independent verifier — a contract or light client can consume it without reimplementing progressive container Merkleization.
3. Field proofs open a path to a verifiable RPC: a client could request one field plus a branch instead of a full block.
4. Broadening coverage from one fixture to a corpus of historical blocks would turn the adapter from validated to exercised.

## 10. Experience

The most useful thing I learned is how narrow the gap is between *internally consistent* and *correct*. The `extra_data` bug passed every check I had written, because I had written the checks against the same understanding as the code. Only a genuinely independent implementation exposed it. Etan's remerkleable was that independent implementation, and having the EIP author's reference to measure against changed the character of the work from "my code agrees with my tests" to "two independent implementations agree."

The second lesson is about reading specifications instead of inferring them. My first generalized index table was a reasonable guess and it was wrong; the specification and the reference implementation both said so within minutes of being consulted. Deriving the answer from the normative source and then confirming it against an independent implementation is a slower first pass and a much faster total path.

## 11. Artifacts

| Artifact | Where |
| --- | --- |
| Field mapping document | [Phase 1 mapping](https://hackmd.io/@jackcc/epf7-project-draft) |
| Merged primitives | #22446, #22689, #22783 |
| Payload adapter and root | #23854, closed |
| Demo, indices, proofs | [`eip7807-demo` branch](https://github.com/JackCC703/erigon/tree/eip7807-demo), `dev/eip7807demo` |
| One-command reproduction | `./dev/eip7807demo/verify.sh` on that branch |
| Independent verification | `dev/eip7807demo/verify/verify.py` on that branch |

## Appendix — Generalized index table

`gindex_no_mix_in` is the field's index inside the progressive Merkle tree alone; `gindex` is the index a verifier uses against the payload root, one level deeper because of the active-fields mix-in.

| # | Field | Type | `gindex_no_mix_in` | `gindex` |
| - | --- | --- | --- | --- |
| 0 | `parent_hash` | `Bytes32` | 2 | 4 |
| 1 | `miner` | `Bytes20` | 24 | 40 |
| 2 | `state_root` | `Bytes32` | 25 | 41 |
| 3 | `transactions` | `ProgressiveList[ProgressiveByteList]` | 26 | 42 |
| 4 | `receipts_root` | `Bytes32` | 27 | 43 |
| 5 | `number` | `uint64` | 224 | 352 |
| 6 | `gas_limits` | `GasAmounts` | 225 | 353 |
| 7 | `gas_used` | `GasAmounts` | 226 | 354 |
| 8 | `timestamp` | `uint64` | 227 | 355 |
| 9 | `extra_data` | `ByteList[32]` | 228 | 356 |
| 10 | `mix_hash` | `Bytes32` | 229 | 357 |
| 11 | `base_fees_per_gas` | `BlobFeesPerGas` | 230 | 358 |
| 12 | `withdrawals` | `ProgressiveList[ProgressiveByteList]` | 231 | 359 |
| 13 | `excess_gas` | `GasAmounts` | 232 | 360 |
| 14 | `parent_beacon_block_root` | `Bytes32` | 233 | 361 |
| 15 | `requests_hash` | `Root` | 234 | 362 |
| 16 | `block_access_list` | `ProgressiveByteList` | 235 | 363 |
| 17 | `slot_number` | `uint64` | 236 | 364 |

The gaps between indices 4 and 24, and between 27 and 224, are the subtree boundaries of the progressive tree. They are the mechanism behind index stability: a field's position depends on how many chunks precede it, not on how many the container eventually holds.
