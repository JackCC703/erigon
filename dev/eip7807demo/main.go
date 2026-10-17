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

// Command eip7807demo prints the EIP-7807 ExecutionPayload root, the generalized
// index table, and Merkle branches for a deterministic local fixture. It runs
// without a node, database, or network access.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/erigontech/erigon/dev/eip7807demo/internal/eip7807"
	"github.com/erigontech/erigon/dev/eip7807demo/internal/report"
	"github.com/erigontech/erigon/execution/types/sszblocks"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "eip7807demo: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	proofFields := flag.String("fields", strings.Join(report.DefaultProofFields, ","),
		"comma-separated payload fields to print Merkle branches for")
	fieldRootsOnly := flag.Bool("gindex", false, "print the generalized index table only")
	flag.Parse()

	block, cfg, err := sszblocks.NewFixture()
	if err != nil {
		return fmt.Errorf("build fixture: %w", err)
	}
	payload, err := sszblocks.ExecutionPayloadFromBlock(block, cfg)
	if err != nil {
		return fmt.Errorf("adapt block: %w", err)
	}

	root, err := payload.HashSSZ()
	if err != nil {
		return fmt.Errorf("hash payload: %w", err)
	}
	// The payload root is the whole point of the demo, so a mismatch is a hard
	// failure rather than a printed warning.
	if got := fmt.Sprintf("0x%x", root); got != report.PayloadRoot {
		return fmt.Errorf("payload root %s does not match pinned root %s", got, report.PayloadRoot)
	}

	if *fieldRootsOnly {
		return writeJSON(gindexTable())
	}

	fields := splitFields(*proofFields)
	if err := verifyBranches(payload, fields, root); err != nil {
		return err
	}
	doc, err := report.New(payload, fields, report.PayloadRoot)
	if err != nil {
		return err
	}
	return writeJSON(doc)
}

type gindexRow struct {
	Index         int    `json:"index"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	GindexNoMixIn string `json:"gindex_no_mix_in"`
	Gindex        string `json:"gindex"`
}

func gindexTable() []gindexRow {
	schema := eip7807.PayloadSchema()
	rows := make([]gindexRow, 0, len(schema))
	for i, field := range schema {
		rows = append(rows, gindexRow{
			Index:         i,
			Name:          field.Name,
			Type:          string(field.Kind),
			GindexNoMixIn: fmt.Sprintf("%d", field.GindexNoMixIn),
			Gindex:        fmt.Sprintf("%d", field.Gindex),
		})
	}
	return rows
}

// verifyBranches folds every branch back into the root before anything is
// printed, so a broken proof fails the command instead of shipping a bad table.
func verifyBranches(payload *sszblocks.ExecutionPayload, fields []string, root [32]byte) error {
	fieldRoots, err := eip7807.FieldRoots(payload)
	if err != nil {
		return err
	}
	for _, name := range fields {
		index, err := eip7807.PayloadFieldIndex(name)
		if err != nil {
			return err
		}
		branch, gindex, err := eip7807.FieldProof(payload, index)
		if err != nil {
			return err
		}
		if err := eip7807.VerifyProof(fieldRoots[index], gindex, branch, root); err != nil {
			return fmt.Errorf("verify %s: %w", name, err)
		}
	}
	return nil
}

func splitFields(list string) []string {
	var fields []string
	for name := range strings.SplitSeq(list, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			fields = append(fields, name)
		}
	}
	return fields
}

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
