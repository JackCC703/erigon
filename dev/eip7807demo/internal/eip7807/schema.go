// Copyright 2026 The Erigon Authors
// This file is part of Erigon.
//
// Erigon is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Erigon is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with Erigon. If not, see <http://www.gnu.org/licenses/>.

// Package eip7807 exposes the EIP-7807 ExecutionPayload schema and the
// generalized indices and Merkle branches derived from it.
package eip7807

import (
	"crypto/sha256"
	"fmt"
	"math"
	"math/bits"

	"github.com/erigontech/erigon/cl/merkle_tree"
	"github.com/erigontech/erigon/common"
	"github.com/erigontech/erigon/execution/types/sszblocks"
)

// ExecutionPayload is the EIP-7807 payload produced by the block adapter.
type ExecutionPayload = sszblocks.ExecutionPayload

// FieldKind classifies a container field by the SSZ type it holds.
type FieldKind string

const (
	KindBytes32      FieldKind = "Bytes32"
	KindBytes20      FieldKind = "Bytes20"
	KindUint64       FieldKind = "uint64"
	KindUint256      FieldKind = "uint256"
	KindExtraData    FieldKind = "ByteList[32]"
	KindTxList       FieldKind = "ProgressiveList[ProgressiveByteList]"
	KindByteList     FieldKind = "ProgressiveByteList"
	KindGasAmounts   FieldKind = "GasAmounts"
	KindBlobFees     FieldKind = "BlobFeesPerGas"
	KindRequestsRoot FieldKind = "Root"
)

// PayloadField describes one field of the EIP-7807 ExecutionPayload.
//
// GindexNoMixIn is the field's generalized index inside the progressive Merkle
// tree taken on its own, with that tree's root at index 1. Gindex additionally
// accounts for the EIP-7495 active-fields mix-in, which prepends a level to the
// tree; the two differ by the leading bit of the path.
type PayloadField struct {
	Name          string
	Kind          FieldKind
	GindexNoMixIn uint64
	Gindex        uint64
}

const payloadFieldCount = 18

// progressiveLeafGindex returns the generalized index of chunk at the given
// position in a progressive Merkle tree whose root is at index 1. Successive
// subtrees hold 1, 4, 16, ... chunks (EIP-7916), so a chunk is a leaf of the
// first subtree with room for it and never moves as later chunks are added.
func progressiveLeafGindex(chunk int) (uint64, error) {
	var offset, capacity, node uint64 = 0, 1, 1
	for {
		if uint64(chunk)-offset < capacity {
			subtreeRoot := node * 2
			return subtreeRoot*capacity + (uint64(chunk) - offset), nil
		}
		offset += capacity
		if capacity > math.MaxUint64/4 {
			return 0, fmt.Errorf("progressive tree capacity overflow at chunk %d", chunk)
		}
		capacity *= 4
		node = node*2 + 1
	}
}

// mixInActiveFields prepends the active-fields level to a generalized index, so
// the index gains a leading path bit while its own path bits are unchanged.
func mixInActiveFields(noMixIn uint64) uint64 {
	return noMixIn + uint64(1)<<(bits.Len64(noMixIn)-1)
}

func progressiveField(fieldIndex int, kind FieldKind, name string) PayloadField {
	noMixIn, err := progressiveLeafGindex(fieldIndex)
	if err != nil {
		panic(err)
	}
	return PayloadField{
		Name:          name,
		Kind:          kind,
		GindexNoMixIn: noMixIn,
		Gindex:        mixInActiveFields(noMixIn),
	}
}

var payloadFields = buildPayloadFields()

func buildPayloadFields() []PayloadField {
	fields := []PayloadField{
		{Name: "parent_hash", Kind: KindBytes32},
		{Name: "miner", Kind: KindBytes20},
		{Name: "state_root", Kind: KindBytes32},
		{Name: "transactions", Kind: KindTxList},
		{Name: "receipts_root", Kind: KindBytes32},
		{Name: "number", Kind: KindUint64},
		{Name: "gas_limits", Kind: KindGasAmounts},
		{Name: "gas_used", Kind: KindGasAmounts},
		{Name: "timestamp", Kind: KindUint64},
		{Name: "extra_data", Kind: KindExtraData},
		{Name: "mix_hash", Kind: KindBytes32},
		{Name: "base_fees_per_gas", Kind: KindBlobFees},
		{Name: "withdrawals", Kind: KindTxList},
		{Name: "excess_gas", Kind: KindGasAmounts},
		{Name: "parent_beacon_block_root", Kind: KindBytes32},
		{Name: "requests_hash", Kind: KindRequestsRoot},
		{Name: "block_access_list", Kind: KindByteList},
		{Name: "slot_number", Kind: KindUint64},
	}
	for i := range fields {
		fields[i] = progressiveField(i, fields[i].Kind, fields[i].Name)
	}
	return fields
}

// PayloadSchema returns the schema of the EIP-7807 ExecutionPayload.
func PayloadSchema() []PayloadField {
	out := make([]PayloadField, len(payloadFields))
	copy(out, payloadFields)
	return out
}

// PayloadFieldCount returns the number of top-level ExecutionPayload fields.
func PayloadFieldCount() int { return payloadFieldCount }

// PayloadFieldIndex looks up a field by name.
func PayloadFieldIndex(name string) (int, error) {
	for i, field := range payloadFields {
		if field.Name == name {
			return i, nil
		}
	}
	return 0, fmt.Errorf("unknown ExecutionPayload field %q", name)
}

// FieldRoots returns the 32-byte SSZ root of every top-level ExecutionPayload
// field, in field order.
func FieldRoots(p *ExecutionPayload) ([][32]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("nil execution payload")
	}
	if len(p.ExtraData) > 32 {
		return nil, fmt.Errorf("extra data length %d exceeds limit 32", len(p.ExtraData))
	}
	transactionsRoot, err := listRoot(p.Transactions)
	if err != nil {
		return nil, fmt.Errorf("hash transactions: %w", err)
	}
	withdrawalsRoot, err := listRoot(p.Withdrawals)
	if err != nil {
		return nil, fmt.Errorf("hash withdrawals: %w", err)
	}
	blockAccessListRoot, err := merkle_tree.ProgressiveByteListRoot(p.BlockAccessList)
	if err != nil {
		return nil, fmt.Errorf("hash block access list: %w", err)
	}
	extraData, err := byteListRoot(p.ExtraData)
	if err != nil {
		return nil, fmt.Errorf("hash extra data: %w", err)
	}
	gasLimits, err := merkle_tree.ProgressiveContainerRootAll(p.GasLimits.Regular, p.GasLimits.Blob)
	if err != nil {
		return nil, fmt.Errorf("hash gas limits: %w", err)
	}
	gasUsed, err := merkle_tree.ProgressiveContainerRootAll(p.GasUsed.Regular, p.GasUsed.Blob)
	if err != nil {
		return nil, fmt.Errorf("hash gas used: %w", err)
	}
	excessGas, err := merkle_tree.ProgressiveContainerRootAll(p.ExcessGas.Regular, p.ExcessGas.Blob)
	if err != nil {
		return nil, fmt.Errorf("hash excess gas: %w", err)
	}
	baseFees, err := p.BaseFeesPerGas.HashSSZ()
	if err != nil {
		return nil, fmt.Errorf("hash base fees per gas: %w", err)
	}
	roots := make([][32]byte, payloadFieldCount)
	set := func(i int, root [32]byte) {
		roots[i] = root
	}
	set(0, p.ParentHash)
	set(1, minerRoot(p.Miner))
	set(2, p.StateRoot)
	set(3, transactionsRoot)
	set(4, p.ReceiptsRoot)
	set(5, merkle_tree.Uint64Root(p.Number))
	set(6, gasLimits)
	set(7, gasUsed)
	set(8, merkle_tree.Uint64Root(p.Timestamp))
	set(9, extraData)
	set(10, p.MixHash)
	set(11, baseFees)
	set(12, withdrawalsRoot)
	set(13, excessGas)
	set(14, p.ParentBeaconBlockRoot)
	set(15, p.RequestsHash)
	set(16, blockAccessListRoot)
	set(17, merkle_tree.Uint64Root(p.SlotNumber))
	return roots, nil
}

// PayloadRoot computes the ExecutionPayload root directly from the per-field
// roots: the progressive container mixes its Merkle root with the active-fields
// bitvector, then the two are hashed together.
func PayloadRoot(p *ExecutionPayload) ([32]byte, error) {
	fieldRoots, err := FieldRoots(p)
	if err != nil {
		return [32]byte{}, err
	}
	merkleRoot, err := merkle_tree.MerkleizeProgressive(fieldRoots)
	if err != nil {
		return [32]byte{}, err
	}
	var activeRoot [32]byte
	for i := range fieldRoots {
		activeRoot[i/8] |= 1 << uint(i%8)
	}
	return hashPair(merkleRoot, activeRoot), nil
}

// FieldProof returns the Merkle branch for one top-level field along with the
// field's generalized index. The branch is ordered leaf-to-root, matching the
// order light clients consume; the active-fields mix-in level is the last
// element, and its sibling is included so the branch length equals the depth of
// the field's generalized index.
func FieldProof(p *ExecutionPayload, fieldIndex int) ([][32]byte, uint64, error) {
	if fieldIndex < 0 || fieldIndex >= payloadFieldCount {
		return nil, 0, fmt.Errorf("payload field index %d out of range", fieldIndex)
	}
	fieldRoots, err := FieldRoots(p)
	if err != nil {
		return nil, 0, err
	}
	proof, err := merkle_tree.ProgressiveContainerProofAll(fieldIndex, schemaRoots(fieldRoots)...)
	if err != nil {
		return nil, 0, err
	}
	return proof, payloadFields[fieldIndex].Gindex, nil
}

// VerifyProof folds a field value back into the payload root using the branch
// returned by FieldProof.
func VerifyProof(fieldRoot [32]byte, gindex uint64, branch [][32]byte, want [32]byte) error {
	if gindex < 2 {
		return fmt.Errorf("generalized index %d has no sibling", gindex)
	}
	if uint64(len(branch)) != uint64(bits.Len64(gindex))-1 {
		return fmt.Errorf("branch length %d does not match generalized index %d", len(branch), gindex)
	}
	root := fieldRoot
	index := gindex
	for _, sibling := range branch {
		if index&1 == 0 {
			root = hashPair(root, sibling)
		} else {
			root = hashPair(sibling, root)
		}
		index >>= 1
	}
	if root != want {
		return fmt.Errorf("proof root %s does not match expected %s",
			common.Hash(root).Hex(), common.Hash(want).Hex())
	}
	return nil
}

func schemaRoots(roots [][32]byte) []any {
	schema := make([]any, len(roots))
	for i := range roots {
		schema[i] = roots[i][:]
	}
	return schema
}

// byteListRoot hashes a bounded ByteList: the packed data is merkleized and its
// length is mixed in, as SSZ specifies for variable-length byte lists. A plain
// BytesRoot would drop the length and produce a different root for the same
// bytes.
func byteListRoot(data []byte) ([32]byte, error) {
	root, err := merkle_tree.BytesRoot(data)
	if err != nil {
		return [32]byte{}, err
	}
	return hashPair(root, merkle_tree.Uint64Root(uint64(len(data)))), nil
}

// minerRoot widens the 20-byte ExecutionAddress to the 32-byte leaf SSZ uses.
func minerRoot(miner common.Address) [32]byte {
	var root [32]byte
	copy(root[:], miner[:])
	return root
}

func listRoot(elements [][]byte) ([32]byte, error) {
	chunks := make([][32]byte, len(elements))
	for i, element := range elements {
		root, err := merkle_tree.ProgressiveByteListRoot(element)
		if err != nil {
			return [32]byte{}, err
		}
		chunks[i] = root
	}
	return merkle_tree.ProgressiveListRoot(chunks, uint64(len(elements)))
}

func hashPair(left, right [32]byte) [32]byte {
	var pair [64]byte
	copy(pair[:32], left[:])
	copy(pair[32:], right[:])
	return sha256.Sum256(pair[:])
}
