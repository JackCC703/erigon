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
	"fmt"

	"github.com/holiman/uint256"

	"github.com/erigontech/erigon/cl/clparams"
	"github.com/erigontech/erigon/cl/cltypes/solid"
	"github.com/erigontech/erigon/common"
	"github.com/erigontech/erigon/execution/chain"
	"github.com/erigontech/erigon/execution/protocol/params"
	"github.com/erigontech/erigon/execution/types"
)

// NewFixture builds the deterministic block that pins the EIP-7807 payload root.
// Tests and the demo command share it, so both entry points hash the same input.
func NewFixture() (*types.BlockWithReceipts, BlockAdapterConfig, error) {
	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	legacyTx := types.NewTransaction(1, to, uint256.NewInt(2), 21_000, uint256.NewInt(3), []byte{0xaa})
	dynamicTx := &types.DynamicFeeTransaction{
		CommonTx: types.CommonTx{
			Nonce:    2,
			GasLimit: 50_000,
			To:       &to,
			Value:    *uint256.NewInt(4),
			Data:     []byte{0xbb, 0xcc},
		},
		ChainID: *uint256.NewInt(1),
		TipCap:  *uint256.NewInt(5),
		FeeCap:  *uint256.NewInt(6),
	}
	receipts := types.Receipts{
		&types.Receipt{Type: types.LegacyTxType, Status: types.ReceiptStatusSuccessful, CumulativeGasUsed: 21_000},
		&types.Receipt{Type: types.DynamicFeeTxType, Status: types.ReceiptStatusFailed, CumulativeGasUsed: 42_000},
	}
	withdrawals := types.Withdrawals{
		&types.Withdrawal{Index: 1, Validator: 2, Address: to, Amount: 3},
	}

	beaconCfg := clparams.MainnetBeaconConfig
	request := &solid.WithdrawalRequest{SourceAddress: to, Amount: 8}
	requestData, err := request.EncodeSSZ(nil)
	if err != nil {
		return nil, BlockAdapterConfig{}, fmt.Errorf("encode withdrawal request: %w", err)
	}
	requests := types.FlatRequests{{Type: byte(beaconCfg.WithdrawalRequestType), RequestData: requestData}}

	bal := make(types.BlockAccessList, 0)
	sidecar := types.NewBlockAccessListSidecar(bal)
	balHash, err := sidecar.Hash()
	if err != nil {
		return nil, BlockAdapterConfig{}, fmt.Errorf("hash block access list: %w", err)
	}

	blobGasUsed := uint64(params.GasPerBlob)
	excessBlobGas := uint64(4096)
	parentBeaconBlockRoot := common.HexToHash("0x4444444444444444444444444444444444444444444444444444444444444444")
	slotNumber := uint64(12345)
	header := &types.Header{
		ParentHash:            common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
		Coinbase:              common.HexToAddress("0x2222222222222222222222222222222222222222"),
		Root:                  common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
		Number:                *uint256.NewInt(99),
		GasLimit:              30_000_000,
		GasUsed:               42_000,
		Time:                  1,
		Extra:                 []byte{0xde, 0xad, 0xbe, 0xef},
		MixDigest:             common.HexToHash("0x5555555555555555555555555555555555555555555555555555555555555555"),
		BaseFee:               uint256.NewInt(1_000_000_000),
		BlobGasUsed:           &blobGasUsed,
		ExcessBlobGas:         &excessBlobGas,
		ParentBeaconBlockRoot: &parentBeaconBlockRoot,
		RequestsHash:          requests.Hash(),
		BlockAccessListHash:   &balHash,
		SlotNumber:            &slotNumber,
	}
	block := types.NewBlock(header, []types.Transaction{legacyTx, dynamicTx}, nil, receipts, withdrawals, sidecar)

	zero := uint64(0)
	chainCfg := &chain.Config{
		CancunTime: &zero,
		BlobSchedule: map[string]*params.BlobConfig{
			"cancun": {Target: 1, Max: 2, BaseFeeUpdateFraction: 10_000},
		},
	}
	regularExcessGas := uint64(7)
	return &types.BlockWithReceipts{
			Block:           block,
			Receipts:        receipts,
			Requests:        requests,
			BlockAccessList: bal,
		}, BlockAdapterConfig{
			ChainConfig:      chainCfg,
			BeaconConfig:     &beaconCfg,
			RegularExcessGas: &regularExcessGas,
		}, nil
}
