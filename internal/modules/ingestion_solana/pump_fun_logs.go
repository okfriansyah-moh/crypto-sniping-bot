package ingestion_solana

// pump_fun_logs.go — decodes Pump.fun "CreateEvent" Anchor events directly
// from logsSubscribe notification log lines, without calling getTransaction.
//
// Why this exists:
//   The Pump.fun on-chain program emits an Anchor event during every successful
//   `create` instruction. Anchor events are surfaced as a `Program data: <b64>`
//   log line whose payload is `event_discriminator(8) || borsh(EventStruct)`.
//   All fields needed for sniping (name, symbol, uri, mint, bondingCurve, user)
//   are encoded in that event, so the entire `getTransaction` round-trip can
//   be eliminated for Pump.fun. Reference implementation:
//     chainstacklabs/pumpfun-bonkfun-bot —
//       learning-examples/listen-new-tokens/listen_logsubscribe_abc.py
//
// Determinism: the decoder is a pure function over the log slice. Same logs
// → same PumpFunLogCreateEvent → same MarketDataDTO → same EventID.

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// pumpFunCreateEventDisc is the Anchor event discriminator for `CreateEvent`,
// computed as sha256("event:CreateEvent")[:8].
var pumpFunCreateEventDisc = func() [8]byte {
	h := sha256.Sum256([]byte("event:CreateEvent"))
	var d [8]byte
	copy(d[:], h[:8])
	return d
}()

// PumpFunLogCreateEvent holds the subset of CreateEvent fields needed by the
// downstream pipeline. The on-chain struct may carry more trailing fields
// (creator, virtualTokenReserves, …); we read only what we need and ignore
// the rest, which is forward-compatible across program upgrades.
type PumpFunLogCreateEvent struct {
	Name         string
	Symbol       string
	URI          string
	Mint         string // base58 pubkey
	BondingCurve string // base58 pubkey
	User         string // base58 pubkey
}

// programDataPrefix is the well-known Solana log prefix for Anchor event
// payloads. Format: "Program data: <base64-encoded bytes>".
const programDataPrefix = "Program data: "

// DecodePumpFunCreateFromLogs scans the log slice for an Anchor `CreateEvent`
// emitted by the Pump.fun program and returns the decoded fields.
//
// Returns (nil, nil) when no CreateEvent is found (not a token-create tx).
// Returns a non-nil error only when a candidate `Program data:` line is
// malformed (truncated borsh, invalid base64). Callers should treat
// (nil, nil) as a normal "skip" and an error as a hard parse failure.
//
// DoS hardening:
//   - At most maxProgramDataLines `Program data:` lines are inspected per
//     call; further matches are ignored. A real CreateEvent appears at most
//     once per tx so a much higher count signals adversarial input.
//   - Each candidate base64 payload is rejected if it exceeds maxLogB64Bytes
//     before allocation, bounding peak memory regardless of provider input.
const (
	maxProgramDataLines = 16
	maxLogB64Bytes      = 16 * 1024
)

func DecodePumpFunCreateFromLogs(logs []string) (*PumpFunLogCreateEvent, error) {
	inspected := 0
	for _, line := range logs {
		idx := strings.Index(line, programDataPrefix)
		if idx < 0 {
			continue
		}
		inspected++
		if inspected > maxProgramDataLines {
			return nil, nil
		}
		b64 := strings.TrimSpace(line[idx+len(programDataPrefix):])
		if b64 == "" || len(b64) > maxLogB64Bytes {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			// Some providers emit padded/unpadded variants — try raw as well.
			raw, err = base64.RawStdEncoding.DecodeString(b64)
			if err != nil {
				continue
			}
		}
		if len(raw) < 8 {
			continue
		}
		if !MatchDiscriminator(raw, pumpFunCreateEventDisc) {
			continue
		}
		event, err := decodePumpFunCreateEventBody(raw[8:])
		if err != nil {
			return nil, fmt.Errorf("pump_fun_create_event: %w", err)
		}
		return event, nil
	}
	return nil, nil
}

// decodePumpFunCreateEventBody decodes the borsh-encoded body of a
// CreateEvent (excluding the 8-byte event discriminator).
//
// Layout (only the prefix we consume; trailing fields are ignored):
//
//	name         : string  (u32 len + utf8)
//	symbol       : string  (u32 len + utf8)
//	uri          : string  (u32 len + utf8)
//	mint         : pubkey  (32)
//	bondingCurve : pubkey  (32)
//	user         : pubkey  (32)
func decodePumpFunCreateEventBody(body []byte) (*PumpFunLogCreateEvent, error) {
	r := NewReader(body)

	name, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("read name: %w", err)
	}
	symbol, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("read symbol: %w", err)
	}
	uri, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("read uri: %w", err)
	}
	mint, err := r.ReadPublicKey()
	if err != nil {
		return nil, fmt.Errorf("read mint: %w", err)
	}
	bondingCurve, err := r.ReadPublicKey()
	if err != nil {
		return nil, fmt.Errorf("read bonding_curve: %w", err)
	}
	user, err := r.ReadPublicKey()
	if err != nil {
		return nil, fmt.Errorf("read user: %w", err)
	}

	return &PumpFunLogCreateEvent{
		Name:         name,
		Symbol:       symbol,
		URI:          uri,
		Mint:         mint,
		BondingCurve: bondingCurve,
		User:         user,
	}, nil
}
