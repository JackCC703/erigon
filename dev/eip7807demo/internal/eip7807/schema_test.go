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

package eip7807

import (
	"math/bits"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/erigontech/erigon/cl/cltypes/solid"
	"github.com/erigontech/erigon/cl/merkle_tree"
	"github.com/erigontech/erigon/common"
	"github.com/erigontech/erigon/execution/types/sszblocks"
)

const payloadRootHex = "0x51951516710af90e8c20092e086f37232c69b558d6ba0035333982e115e1f627"

func fixturePayload(t *testing.T) *ExecutionPayload {
	t.Helper()
	block, cfg, err := sszblocks.NewFixture()
	require.NoError(t, err)
	payload, err := sszblocks.ExecutionPayloadFromBlock(block, cfg)
	require.NoError(t, err)
	return payload
}

// TestPayloadRootMatchesAdapter checks the field-by-field recomputation against
// the independent path the adapter takes through cl/merkle_tree.
func TestPayloadRootMatchesAdapter(t *testing.T) {
	payload := fixturePayload(t)
	want, err := payload.HashSSZ()
	require.NoError(t, err)
	require.Equal(t, payloadRootHex, common.Hash(want).Hex())

	got, err := PayloadRoot(payload)
	require.NoError(t, err)
	require.Equal(t, common.Hash(want).Hex(), common.Hash(got).Hex())
}

// TestPayloadGindices pins the generalized index of every top-level field, both
// with and without the EIP-7495 active-fields mix-in. The three values the
// project proposal predicted are asserted by name as well, so a change in the
// derivation cannot silently agree with a wrong table.
func TestPayloadGindices(t *testing.T) {
	want := map[string][2]uint64{
		"parent_hash":              {2, 4},
		"miner":                    {24, 40},
		"state_root":               {25, 41},
		"transactions":             {26, 42},
		"receipts_root":            {27, 43},
		"number":                   {224, 352},
		"gas_limits":               {225, 353},
		"gas_used":                 {226, 354},
		"timestamp":                {227, 355},
		"extra_data":               {228, 356},
		"mix_hash":                 {229, 357},
		"base_fees_per_gas":        {230, 358},
		"withdrawals":              {231, 359},
		"excess_gas":               {232, 360},
		"parent_beacon_block_root": {233, 361},
		"requests_hash":            {234, 362},
		"block_access_list":        {235, 363},
		"slot_number":              {236, 364},
	}
	schema := PayloadSchema()
	require.Len(t, schema, payloadFieldCount)
	require.Len(t, want, payloadFieldCount)
	for _, field := range schema {
		expected, ok := want[field.Name]
		require.True(t, ok, "unexpected field %q", field.Name)
		require.Equal(t, expected[0], field.GindexNoMixIn, "%s gindex without mix-in", field.Name)
		require.Equal(t, expected[1], field.Gindex, "%s gindex", field.Name)
	}
}

// TestGindexIsStableWhenFieldsAreAppended pins the property progressive
// containers exist for: growing the container must not move any existing field,
// so a proof issued for today's 18-field payload stays valid for a longer one.
func TestGindexIsStableWhenFieldsAreAppended(t *testing.T) {
	grown := make([]PayloadField, payloadFieldCount+4)
	for i := range grown {
		grown[i] = progressiveField(i, KindUint64, "extra")
	}
	schema := PayloadSchema()
	for i, field := range schema {
		require.Equal(t, field.Gindex, grown[i].Gindex, "%s moved when fields were appended", field.Name)
	}

	transactions, err := progressiveLeafGindex(3)
	require.NoError(t, err)
	slotNumber, err := progressiveLeafGindex(17)
	require.NoError(t, err)
	require.NotEqual(t, transactions, slotNumber, "distinct chunks must not collide")
}

// TestMixInActiveFieldsAddsOneLevel documents the arithmetic the gindex table
// relies on: the mix-in prepends a path level, so the index shifts by the
// power of two at its own depth.
func TestMixInActiveFieldsAddsOneLevel(t *testing.T) {
	for _, noMixIn := range []uint64{2, 24, 25, 26, 27, 224, 236} {
		mixed := mixInActiveFields(noMixIn)
		require.Equal(t, noMixIn+uint64(1)<<(bits.Len64(noMixIn)-1), mixed, "no mix-in %d", noMixIn)
	}
}

// TestExtraDataRootMatchesLengthMixIn is a regression test: extra_data is a
// ByteList, so its root must mix the length in. Hashing the bytes alone yields a
// different root and breaks the payload root.
func TestExtraDataRootMatchesLengthMixIn(t *testing.T) {
	payload := fixturePayload(t)

	extraData := solid.NewExtraData()
	extraData.SetBytes(payload.ExtraData)
	want, err := extraData.HashSSZ()
	require.NoError(t, err)

	got, err := byteListRoot(payload.ExtraData)
	require.NoError(t, err)
	require.Equal(t, common.Hash(want).Hex(), common.Hash(got).Hex())

	withoutLength, err := merkle_tree.BytesRoot(payload.ExtraData)
	require.NoError(t, err)
	require.NotEqual(t, common.Hash(want).Hex(), common.Hash(withoutLength).Hex())
}

// TestFieldProofsFoldBackIntoRoot is the acceptance test for the demo: every
// field's branch must rebuild the payload root from nothing but the field root,
// the gindex, and the branch.
func TestFieldProofsFoldBackIntoRoot(t *testing.T) {
	payload := fixturePayload(t)
	root, err := payload.HashSSZ()
	require.NoError(t, err)
	fieldRoots, err := FieldRoots(payload)
	require.NoError(t, err)

	schema := PayloadSchema()
	for i, field := range schema {
		branch, gindex, err := FieldProof(payload, i)
		require.NoError(t, err, field.Name)
		require.Equal(t, field.Gindex, gindex, "%s gindex", field.Name)
		require.Equal(t, bits.Len64(gindex)-1, len(branch), "%s branch length", field.Name)
		require.NoError(t, VerifyProof(fieldRoots[i], gindex, branch, root), "%s proof", field.Name)
	}
}

func TestFieldProofRejectsOutOfRangeIndex(t *testing.T) {
	payload := fixturePayload(t)
	for _, index := range []int{-1, payloadFieldCount} {
		_, _, err := FieldProof(payload, index)
		require.Error(t, err, "index %d", index)
	}
}

func TestVerifyProofRejectsWrongBranch(t *testing.T) {
	payload := fixturePayload(t)
	root, err := payload.HashSSZ()
	require.NoError(t, err)
	fieldRoots, err := FieldRoots(payload)
	require.NoError(t, err)

	branch, gindex, err := FieldProof(payload, 0)
	require.NoError(t, err)
	require.NoError(t, VerifyProof(fieldRoots[0], gindex, branch, root))

	var tampered [][32]byte
	tampered = append(tampered, branch...)
	tampered[0][0] ^= 0xff
	require.Error(t, VerifyProof(fieldRoots[0], gindex, tampered, root))

	var short [][32]byte
	short = append(short, branch[:len(branch)-1]...)
	require.Error(t, VerifyProof(fieldRoots[0], gindex, short, root))
}
