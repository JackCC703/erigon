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

package sszblocks

import (
	"bytes"
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/erigontech/erigon/common"
	"github.com/erigontech/erigon/execution/protocol/params"
	"github.com/erigontech/erigon/execution/types"
)

const eip7807FixtureRevision = "ethereum/EIPs EIPS/eip-7807.md blob 3f2dc991ac65153842fd9b894c04ba590d58d3e6, retrieved 2026-09-07"

func TestExecutionPayloadFromBlockAndHashSSZ(t *testing.T) {
	t.Parallel()

	block, cfg := payloadFixture(t)
	payload, err := ExecutionPayloadFromBlock(block, cfg)
	require.NoError(t, err)

	require.Equal(t, block.Block.ParentHash(), payload.ParentHash)
	require.Equal(t, block.Block.Coinbase(), payload.Miner)
	require.Equal(t, block.Block.Root(), payload.StateRoot)
	require.Len(t, payload.Transactions, 2)
	require.Equal(t, byte(types.DynamicFeeTxType), payload.Transactions[1][0])
	require.Len(t, payload.Withdrawals, 1)
	require.Equal(t, []byte{0xc0}, payload.BlockAccessList)
	require.Equal(t, uint64(2*params.GasPerBlob), payload.GasLimits.Blob)
	require.Equal(t, uint64(7), payload.ExcessGas.Regular)
	require.Equal(t, uint64(4096), payload.ExcessGas.Blob)
	require.Equal(t, uint64(12345), payload.SlotNumber)

	root, err := payload.HashSSZ()
	require.NoError(t, err)
	require.Equal(t,
		common.HexToHash("0x51951516710af90e8c20092e086f37232c69b558d6ba0035333982e115e1f627"),
		common.Hash(root),
		eip7807FixtureRevision,
	)

	require.NotEqual(t, block.Block.ReceiptHash(), payload.ReceiptsRoot)
	require.NotEqual(t, *block.Block.RequestsHash(), payload.RequestsHash)
	require.Equal(t, common.HexToHash("0xb1c4c413201f39ef0e4528edbd42e595a5258578a22e8634a9dee79684fdf245"), payload.RequestsHash)
}

func TestExecutionPayloadFromBlockRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*types.BlockWithReceipts, *BlockAdapterConfig)
		wantErr string
	}{
		{
			name: "RejectsMissingRequests",
			mutate: func(block *types.BlockWithReceipts, cfg *BlockAdapterConfig) {
				block.Requests = nil
			},
			wantErr: "block is missing execution requests",
		},
		{
			name: "RejectsMissingRequestsHash",
			mutate: func(block *types.BlockWithReceipts, cfg *BlockAdapterConfig) {
				header := block.Block.Header()
				header.RequestsHash = nil
				replaceBlockHeader(block, header)
			},
			wantErr: "header is missing execution requests hash",
		},
		{
			name: "RejectsRequestsHashMismatch",
			mutate: func(block *types.BlockWithReceipts, cfg *BlockAdapterConfig) {
				block.Requests = append(types.FlatRequests(nil), block.Requests...)
				block.Requests[0].RequestData = bytes.Clone(block.Requests[0].RequestData)
				block.Requests[0].RequestData[len(block.Requests[0].RequestData)-8]++
			},
			wantErr: "execution requests hash mismatch",
		},
		{
			name: "RejectsMissingSlotNumber",
			mutate: func(block *types.BlockWithReceipts, cfg *BlockAdapterConfig) {
				header := block.Block.Header()
				header.SlotNumber = nil
				replaceBlockHeader(block, header)
			},
			wantErr: "header is missing slot number",
		},
		{
			name: "RejectsOversizedBlockNumber",
			mutate: func(block *types.BlockWithReceipts, cfg *BlockAdapterConfig) {
				header := block.Block.Header()
				header.Number.Lsh(uint256.NewInt(1), 64)
				replaceBlockHeader(block, header)
			},
			wantErr: "block number does not fit uint64",
		},
		{
			name: "RejectsReceiptCountMismatch",
			mutate: func(block *types.BlockWithReceipts, cfg *BlockAdapterConfig) {
				block.Receipts = block.Receipts[:1]
			},
			wantErr: "transaction and receipt counts differ: 2 != 1",
		},
		{
			name: "RejectsTransactionHashMismatch",
			mutate: func(block *types.BlockWithReceipts, cfg *BlockAdapterConfig) {
				block.Block.Transactions()[0] = block.Block.Transactions()[1]
			},
			wantErr: "transaction hash mismatch",
		},
		{
			name: "RejectsMissingWithdrawalsHash",
			mutate: func(block *types.BlockWithReceipts, cfg *BlockAdapterConfig) {
				header := block.Block.Header()
				header.WithdrawalsHash = nil
				block.Block = types.NewBlockFromNetwork(header, &types.Body{
					Transactions: block.Block.Transactions(),
					Withdrawals:  block.Block.Withdrawals(),
				}, block.Block.BlockAccessListSidecar())
			},
			wantErr: "header is missing withdrawals hash",
		},
		{
			name: "RejectsWithdrawalsHashMismatch",
			mutate: func(block *types.BlockWithReceipts, cfg *BlockAdapterConfig) {
				block.Block.Withdrawals()[0].Amount++
			},
			wantErr: "withdrawals hash mismatch",
		},
		{
			name: "RejectsReceiptHashMismatch",
			mutate: func(block *types.BlockWithReceipts, cfg *BlockAdapterConfig) {
				block.Receipts[0].Status = types.ReceiptStatusFailed
			},
			wantErr: "receipt hash mismatch",
		},
		{
			name: "RequiresRegularExcessGas",
			mutate: func(block *types.BlockWithReceipts, cfg *BlockAdapterConfig) {
				cfg.RegularExcessGas = nil
			},
			wantErr: "regular excess gas is required",
		},
		{
			name: "RejectsMismatchedBlockAccessLists",
			mutate: func(block *types.BlockWithReceipts, cfg *BlockAdapterConfig) {
				block.BlockAccessList = types.BlockAccessList{{}}
			},
			wantErr: "block access list result differs from block sidecar",
		},
		{
			name: "RejectsDuplicateRequestType",
			mutate: func(block *types.BlockWithReceipts, cfg *BlockAdapterConfig) {
				block.Requests = append(block.Requests, block.Requests[0])
				header := block.Block.Header()
				header.RequestsHash = block.Requests.Hash()
				replaceBlockHeader(block, header)
			},
			wantErr: "decode execution requests: execution request type 1 is not strictly ascending",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			block, cfg := payloadFixture(t)
			tt.mutate(block, &cfg)
			_, err := ExecutionPayloadFromBlock(block, cfg)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestExecutionPayloadFromBlockUsesBlockAccessListSidecar(t *testing.T) {
	t.Parallel()

	block, cfg := payloadFixture(t)
	block.BlockAccessList = nil
	payload, err := ExecutionPayloadFromBlock(block, cfg)
	require.NoError(t, err)
	require.Equal(t, []byte{0xc0}, payload.BlockAccessList)
}

func TestExecutionPayloadHashSSZRejectsOversizedExtraData(t *testing.T) {
	t.Parallel()

	payload := &ExecutionPayload{ExtraData: make([]byte, 33)}
	_, err := payload.HashSSZ()
	require.EqualError(t, err, "extra data length 33 exceeds limit 32")
}

func payloadFixture(t *testing.T) (*types.BlockWithReceipts, BlockAdapterConfig) {
	t.Helper()

	block, cfg, err := NewFixture()
	require.NoError(t, err)
	return block, cfg
}

func replaceBlockHeader(block *types.BlockWithReceipts, header *types.Header) {
	block.Block = types.NewBlock(
		header,
		block.Block.Transactions(),
		nil,
		block.Receipts,
		block.Block.Withdrawals(),
		block.Block.BlockAccessListSidecar(),
	)
}
