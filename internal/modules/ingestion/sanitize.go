package ingestion

import "regexp"

// rpcKeyPathRe matches API keys embedded as a path segment after /v<N>/.
// Covers Infura  wss://mainnet.infura.io/ws/v3/<KEY>
//         Alchemy  https://eth-mainnet.g.alchemy.com/v2/<KEY>
//         QuickNode https://shy-aged-sky.quiknode.pro/<KEY>/
var rpcKeyPathRe = regexp.MustCompile(`(?i)(/v\d+/)([a-zA-Z0-9_\-]{20,})`)

// rpcKeyQueryRe matches API keys embedded as query parameters.
// Covers ?token=KEY &key=KEY &apikey=KEY &api_key=KEY
var rpcKeyQueryRe = regexp.MustCompile(`(?i)([?&](?:token|key|apikey|api_key)=)([a-zA-Z0-9_\-]+)`)

// rpcKeyTrailingRe matches QuickNode-style keys as a trailing path segment
// with no /v<N>/ prefix: https://<host>/<32+char key>/
var rpcKeyTrailingRe = regexp.MustCompile(`(?i)(://[^/]+/)([a-zA-Z0-9]{32,})(/|$)`)

// sanitizeEndpoint masks API keys embedded in common RPC URL patterns so that
// the stored RpcEndpoint field is safe to persist in the database and appear
// in log output without leaking credentials.
//
// All transformations are non-destructive: the scheme, host, and path structure
// are preserved so that the masked URL still identifies the provider.
func sanitizeEndpoint(rawURL string) string {
	s := rpcKeyPathRe.ReplaceAllString(rawURL, "${1}[REDACTED]")
	s = rpcKeyQueryRe.ReplaceAllString(s, "${1}[REDACTED]")
	s = rpcKeyTrailingRe.ReplaceAllString(s, "${1}[REDACTED]${3}")
	return s
}
