package ingestion_solana

import (
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
)

// buildPumpFunCreateEventLogLine assembles a synthetic Anchor `CreateEvent`
// payload using the canonical discriminator + borsh layout, and wraps it in
// the standard `Program data:` log-line shape.
func buildPumpFunCreateEventLogLine(t *testing.T, name, symbol, uri string, mint, bonding, user [32]byte) string {
	t.Helper()
	var payload []byte
	payload = append(payload, pumpFunCreateEventDisc[:]...)
	for _, s := range []string{name, symbol, uri} {
		var l [4]byte
		binary.LittleEndian.PutUint32(l[:], uint32(len(s)))
		payload = append(payload, l[:]...)
		payload = append(payload, []byte(s)...)
	}
	payload = append(payload, mint[:]...)
	payload = append(payload, bonding[:]...)
	payload = append(payload, user[:]...)
	return "Program data: " + base64.StdEncoding.EncodeToString(payload)
}

func TestDecodePumpFunCreateFromLogs_HappyPath(t *testing.T) {
	mint := [32]byte{1}
	bonding := [32]byte{2}
	user := [32]byte{3}

	logs := []string{
		"Program 6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P invoke [1]",
		"Program log: Instruction: Create",
		"Program 11111111111111111111111111111111 invoke [2]",
		"Program 11111111111111111111111111111111 success",
		buildPumpFunCreateEventLogLine(t, "TokenName", "TKN", "ipfs://abc", mint, bonding, user),
		"Program 6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P success",
	}

	got, err := DecodePumpFunCreateFromLogs(logs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatalf("expected event, got nil")
	}
	if got.Name != "TokenName" || got.Symbol != "TKN" || got.URI != "ipfs://abc" {
		t.Errorf("strings mismatch: %+v", got)
	}
	if got.Mint == "" || got.BondingCurve == "" || got.User == "" {
		t.Errorf("pubkeys empty: %+v", got)
	}
}

func TestDecodePumpFunCreateFromLogs_NoCreateEvent(t *testing.T) {
	logs := []string{
		"Program 6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P invoke [1]",
		"Program log: Instruction: Buy", // swap, not a create
		"Program 6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P success",
	}
	got, err := DecodePumpFunCreateFromLogs(logs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil event, got %+v", got)
	}
}

func TestDecodePumpFunCreateFromLogs_ATAOnlyCreate(t *testing.T) {
	// ATA program also logs "Instruction: Create" but emits no Anchor event
	// matching pumpFunCreateEventDisc — must be safely skipped.
	logs := []string{
		"Program ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL invoke [1]",
		"Program log: Instruction: Create",
		"Program data: " + base64.StdEncoding.EncodeToString([]byte("not-an-anchor-event")),
		"Program ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL success",
	}
	got, err := DecodePumpFunCreateFromLogs(logs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil event for ATA-only create, got %+v", got)
	}
}

func TestDecodePumpFunCreateFromLogs_TruncatedPayload(t *testing.T) {
	// Discriminator matches but body is short — should surface a hard error.
	short := append([]byte{}, pumpFunCreateEventDisc[:]...)
	short = append(short, 0x05, 0x00, 0x00, 0x00) // claims 5-byte name, no body
	logs := []string{
		"Program data: " + base64.StdEncoding.EncodeToString(short),
	}
	if _, err := DecodePumpFunCreateFromLogs(logs); err == nil {
		t.Fatalf("expected error for truncated event, got nil")
	}
}

func TestDecodePumpFunCreateFromLogs_Deterministic(t *testing.T) {
	mint := [32]byte{9, 9, 9}
	bonding := [32]byte{8}
	user := [32]byte{7}
	logs := []string{
		buildPumpFunCreateEventLogLine(t, "A", "B", "C", mint, bonding, user),
	}
	first, err := DecodePumpFunCreateFromLogs(logs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := DecodePumpFunCreateFromLogs(logs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *first != *second {
		t.Errorf("non-deterministic decode: %+v vs %+v", first, second)
	}
}

// TestDecodePumpFunCreateFromLogs_OversizePayloadRejected verifies that a
// `Program data:` line whose base64 payload exceeds maxLogB64Bytes is
// silently skipped without allocating the decoded buffer (DoS hardening).
func TestDecodePumpFunCreateFromLogs_OversizePayloadRejected(t *testing.T) {
	// 32 KB of valid base64 chars — well over the 16 KB cap.
	huge := strings.Repeat("A", 32*1024)
	logs := []string{
		"Program data: " + huge,
	}
	got, err := DecodePumpFunCreateFromLogs(logs)
	if err != nil {
		t.Fatalf("expected no error for oversize payload (silent skip), got: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil event for oversize payload, got %+v", got)
	}
}

// TestDecodePumpFunCreateFromLogs_TooManyProgramDataLinesRejected verifies
// that a notification carrying more than maxProgramDataLines `Program data:`
// lines is short-circuited rather than scanned exhaustively.
func TestDecodePumpFunCreateFromLogs_TooManyProgramDataLinesRejected(t *testing.T) {
	logs := make([]string, 0, 64)
	for i := 0; i < 64; i++ {
		logs = append(logs, "Program data: AAECAwQFBgc=") // 8 bytes < disc len; never matches
	}
	got, err := DecodePumpFunCreateFromLogs(logs)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil event when no CreateEvent present, got %+v", got)
	}
}
