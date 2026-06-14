package rpc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseTransactionSubscribePayload_RaydiumInitialize2Fixture(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "transaction_subscribe_initialize2.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var envelope txNotificationEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	r := envelope.Params.Result
	tx, err := ParseTransactionSubscribePayload(r.Signature, r.Slot, r.Transaction)
	if err != nil {
		t.Fatalf("ParseTransactionSubscribePayload: %v", err)
	}
	if tx == nil {
		t.Fatal("expected non-nil transaction")
	}
	if tx.Signature != r.Signature {
		t.Errorf("Signature: got %q want %q", tx.Signature, r.Signature)
	}
	if tx.Slot != r.Slot {
		t.Errorf("Slot: got %d want %d", tx.Slot, r.Slot)
	}
	if tx.BlockTime != 1_700_000_000 {
		t.Errorf("BlockTime: got %d want 1700000000", tx.BlockTime)
	}
	if len(tx.Instructions) != 1 {
		t.Fatalf("Instructions: got %d want 1", len(tx.Instructions))
	}
	instr := tx.Instructions[0]
	if instr.ProgramID != "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8" {
		t.Errorf("ProgramID: got %q", instr.ProgramID)
	}
	if len(instr.Data) < 1 || instr.Data[0] != 1 {
		t.Errorf("expected Initialize2 tag 1, got %v", instr.Data)
	}
}
