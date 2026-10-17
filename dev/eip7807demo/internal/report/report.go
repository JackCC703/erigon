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

// Package report renders the demo command's JSON report.
package report

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/erigontech/erigon/common"
	"github.com/erigontech/erigon/dev/eip7807demo/internal/eip7807"
	"github.com/erigontech/erigon/execution/types/sszblocks"
)

// SchemaRevision pins the EIP-7807 document the fixture root was derived from.
const SchemaRevision = "3f2dc991ac65153842fd9b894c04ba590d58d3e6"

// PayloadRoot is the ExecutionPayload root that every entry point must reproduce.
const PayloadRoot = "0x51951516710af90e8c20092e086f37232c69b558d6ba0035333982e115e1f627"

// RequestsRoot is the ExecutionRequests root carried in the requests_hash field.
const RequestsRoot = "0xb1c4c413201f39ef0e4528edbd42e595a5258578a22e8634a9dee79684fdf245"

// DefaultProofFields are the fields the demo prints Merkle branches for.
var DefaultProofFields = []string{"parent_hash", "transactions", "receipts_root", "withdrawals"}

// Report is the JSON document the demo command prints.
type Report struct {
	SchemaRevision string       `json:"schema_revision"`
	PayloadRoot    string       `json:"payload_root"`
	RequestsRoot   string       `json:"requests_root"`
	Fields         []FieldEntry `json:"fields"`
	Proofs         []ProofEntry `json:"proofs"`
}

// FieldEntry is one row of the payload generalized index table.
type FieldEntry struct {
	Index         int    `json:"index"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	GindexNoMixIn string `json:"gindex_no_mix_in"`
	Gindex        string `json:"gindex"`
	ValueHex      string `json:"value_hex"`
	Root          string `json:"root"`
}

// ProofEntry is a Merkle branch for one payload field.
type ProofEntry struct {
	Field        string   `json:"field"`
	Gindex       string   `json:"gindex"`
	BranchLength int      `json:"branch_length"`
	Branch       []string `json:"branch"`
}

// New assembles the full report for a payload. When expectedRoot is non-empty
// it must equal the computed payload root, so a caller cannot publish a report
// whose root drifted from the pinned specification revision.
func New(p *sszblocks.ExecutionPayload, proofFields []string, expectedRoot string) (*Report, error) {
	root, err := eip7807.PayloadRoot(p)
	if err != nil {
		return nil, err
	}
	fieldRoots, err := eip7807.FieldRoots(p)
	if err != nil {
		return nil, err
	}
	if expectedRoot != "" && !strings.EqualFold(expectedRoot, common.Hash(root).Hex()) {
		return nil, fmt.Errorf("payload root %s does not match expected %s", common.Hash(root).Hex(), expectedRoot)
	}
	schema := eip7807.PayloadSchema()
	report := &Report{
		SchemaRevision: SchemaRevision,
		PayloadRoot:    common.Hash(root).Hex(),
		RequestsRoot:   p.RequestsHash.Hex(),
		Fields:         make([]FieldEntry, 0, len(schema)),
	}
	for i, field := range schema {
		value, err := fieldValue(p, field.Name)
		if err != nil {
			return nil, err
		}
		report.Fields = append(report.Fields, FieldEntry{
			Index:         i,
			Name:          field.Name,
			Type:          string(field.Kind),
			GindexNoMixIn: fmt.Sprintf("%d", field.GindexNoMixIn),
			Gindex:        fmt.Sprintf("%d", field.Gindex),
			ValueHex:      value,
			Root:          common.Hash(fieldRoots[i]).Hex(),
		})
	}
	for _, name := range proofFields {
		index, err := eip7807.PayloadFieldIndex(name)
		if err != nil {
			return nil, err
		}
		branch, gindex, err := eip7807.FieldProof(p, index)
		if err != nil {
			return nil, err
		}
		entry := ProofEntry{
			Field:        name,
			Gindex:       fmt.Sprintf("%d", gindex),
			BranchLength: len(branch),
			Branch:       make([]string, 0, len(branch)),
		}
		for _, node := range branch {
			entry.Branch = append(entry.Branch, common.Hash(node).Hex())
		}
		report.Proofs = append(report.Proofs, entry)
	}
	return report, nil
}

// fieldValue renders the fixture's value for one payload field. Byte lists and
// nested containers expand to their pre-image; the output is display-only and
// is not hashed.
func fieldValue(p *sszblocks.ExecutionPayload, name string) (string, error) {
	switch name {
	case "parent_hash":
		return p.ParentHash.Hex(), nil
	case "miner":
		return p.Miner.Hex(), nil
	case "state_root":
		return p.StateRoot.Hex(), nil
	case "transactions":
		return listHex(p.Transactions)
	case "receipts_root":
		return p.ReceiptsRoot.Hex(), nil
	case "number":
		return fmt.Sprintf("%d", p.Number), nil
	case "gas_limits":
		return gasAmountsHex(p.GasLimits), nil
	case "gas_used":
		return gasAmountsHex(p.GasUsed), nil
	case "timestamp":
		return fmt.Sprintf("%d", p.Timestamp), nil
	case "extra_data":
		return hexPrefixed(p.ExtraData), nil
	case "mix_hash":
		return p.MixHash.Hex(), nil
	case "base_fees_per_gas":
		return fmt.Sprintf("regular=%s blob=%s", p.BaseFeesPerGas.Regular.Dec(), p.BaseFeesPerGas.Blob.Dec()), nil
	case "withdrawals":
		return listHex(p.Withdrawals)
	case "excess_gas":
		return gasAmountsHex(p.ExcessGas), nil
	case "parent_beacon_block_root":
		return p.ParentBeaconBlockRoot.Hex(), nil
	case "requests_hash":
		return p.RequestsHash.Hex(), nil
	case "block_access_list":
		return hexPrefixed(p.BlockAccessList), nil
	case "slot_number":
		return fmt.Sprintf("%d", p.SlotNumber), nil
	default:
		return "", fmt.Errorf("unknown ExecutionPayload field %q", name)
	}
}

func gasAmountsHex(amounts sszblocks.GasAmounts) string {
	return fmt.Sprintf("regular=%d blob=%d", amounts.Regular, amounts.Blob)
}

func listHex(elements [][]byte) (string, error) {
	var out strings.Builder
	out.WriteString("[")
	for i, element := range elements {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(hexPrefixed(element))
	}
	out.WriteString("]")
	return out.String(), nil
}

func hexPrefixed(data []byte) string {
	return "0x" + hex.EncodeToString(data)
}
