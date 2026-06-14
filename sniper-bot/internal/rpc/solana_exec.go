// solana_exec.go — execution-side Solana RPC methods added to SolanaClient.
//
// These methods extend SolanaClient to also satisfy execution_solana.SolanaClient,
// enabling the same concrete transport instance to be shared by both the ingestion
// module (logsSubscribe / getTransaction) and the execution module (sendTransaction /
// getSignatureStatuses).
//
// Architecture:
//   - No database imports.
//   - Uses the same httpRPC helper as the ingestion methods — bounded timeout,
//     4 MiB response cap, structured error wrapping.
//   - GetSignatureStatus returns a pointer into execution_solana so the execution
//     module can use the type directly without an additional adapter.
package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"crypto-sniping-bot/sniper-bot/internal/modules/execution_solana"
)

// SendTransaction submits a base64-encoded signed transaction to Solana via
// the sendTransaction JSON-RPC method.
// Returns the transaction signature on success.
// Implements execution_solana.SolanaClient.
func (c *SolanaClient) SendTransaction(ctx context.Context, txBase64 string) (string, error) {
	params := []interface{}{
		txBase64,
		map[string]interface{}{"encoding": "base64"},
	}
	var sig string
	if err := c.httpRPC(ctx, "sendTransaction", params, &sig); err != nil {
		return "", fmt.Errorf("solana_client: sendTransaction: %w", err)
	}
	return sig, nil
}

// GetSignatureStatus fetches the confirmation status for a single transaction
// signature via getSignatureStatuses.
// Returns nil (no error) when the signature is not yet visible on the network.
// Implements execution_solana.SolanaClient.
func (c *SolanaClient) GetSignatureStatus(ctx context.Context, signature string) (*execution_solana.SignatureStatus, error) {
	params := []interface{}{
		[]string{signature},
		map[string]interface{}{"searchTransactionHistory": false},
	}

	var result struct {
		Value []json.RawMessage `json:"value"`
	}
	if err := c.httpRPC(ctx, "getSignatureStatuses", params, &result); err != nil {
		return nil, fmt.Errorf("solana_client: getSignatureStatuses: %w", err)
	}

	if len(result.Value) == 0 || string(result.Value[0]) == "null" {
		return nil, nil // not yet visible
	}

	var raw struct {
		Slot               uint64      `json:"slot"`
		Confirmations      *int64      `json:"confirmations"`
		Err                interface{} `json:"err"`
		ConfirmationStatus string      `json:"confirmationStatus"`
	}
	if err := json.Unmarshal(result.Value[0], &raw); err != nil {
		return nil, fmt.Errorf("solana_client: parse signature status: %w", err)
	}

	return &execution_solana.SignatureStatus{
		Slot:               raw.Slot,
		Confirmations:      raw.Confirmations,
		Err:                raw.Err,
		ConfirmationStatus: raw.ConfirmationStatus,
	}, nil
}
