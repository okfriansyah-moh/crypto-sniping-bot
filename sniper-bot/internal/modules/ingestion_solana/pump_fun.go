package ingestion_solana

// pump_fun.go — Pump.fun program instruction decoder.
//
// Program ID: 6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P
//
// The "create" instruction has the Anchor discriminator:
//   sha256("global:create")[:8] = [24, 30, 200, 40, 5, 28, 89, 180]
//
// Instruction data layout (after 8-byte discriminator):
//   name   : string  (borsh: u32 len + bytes)
//   symbol : string  (borsh: u32 len + bytes)
//   uri    : string  (borsh: u32 len + bytes)

import "fmt"

// PumpFunCreateDiscriminator is the Anchor discriminator for Pump.fun "create".
var PumpFunCreateDiscriminator = [8]byte{24, 30, 200, 40, 5, 28, 89, 180}

// PumpFunCreateEvent holds the decoded fields of a Pump.fun token creation.
type PumpFunCreateEvent struct {
	Name   string
	Symbol string
	URI    string
}

// DecodePumpFunCreate decodes a Pump.fun create instruction from raw instruction data.
// Returns an error if the data does not match the expected format.
func DecodePumpFunCreate(data []byte) (*PumpFunCreateEvent, error) {
	if !MatchDiscriminator(data, PumpFunCreateDiscriminator) {
		return nil, fmt.Errorf("not a pump_fun create instruction")
	}

	r := NewReader(data[8:]) // skip 8-byte discriminator

	name, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("pump_fun create: read name: %w", err)
	}
	symbol, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("pump_fun create: read symbol: %w", err)
	}
	uri, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("pump_fun create: read uri: %w", err)
	}

	return &PumpFunCreateEvent{
		Name:   name,
		Symbol: symbol,
		URI:    uri,
	}, nil
}
