package ingestion

import "testing"

func TestSanitizeEndpoint(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "infura_wss_v3",
			input: "wss://mainnet.infura.io/ws/v3/abc123def456abc123def456abc123de",
			want:  "wss://mainnet.infura.io/ws/v3/[REDACTED]",
		},
		{
			name:  "alchemy_https_v2",
			input: "https://eth-mainnet.g.alchemy.com/v2/aBcDeFgHiJkLmNoPqRsTuVwXyZ123456",
			want:  "https://eth-mainnet.g.alchemy.com/v2/[REDACTED]",
		},
		{
			name:  "quicknode_trailing_key",
			input: "https://shy-aged-sky.quiknode.pro/abcdef1234567890abcdef1234567890abcdef12/",
			want:  "https://shy-aged-sky.quiknode.pro/[REDACTED]/",
		},
		{
			name:  "query_param_token",
			input: "https://rpc.example.com/eth?token=supersecrettoken12345",
			want:  "https://rpc.example.com/eth?token=[REDACTED]",
		},
		{
			name:  "query_param_apikey",
			input: "https://rpc.example.com/eth?apikey=supersecrettoken12345",
			want:  "https://rpc.example.com/eth?apikey=[REDACTED]",
		},
		{
			name:  "helius_api_key_hyphen",
			input: "https://mainnet.helius-rpc.com/?api-key=ca537ca0-122c-4e5e-86f6-3449c3ed76cf",
			want:  "https://mainnet.helius-rpc.com/?api-key=[REDACTED]",
		},
		{
			name:  "no_key_passthrough",
			input: "http://localhost:8545",
			want:  "http://localhost:8545",
		},
		{
			name:  "empty_string",
			input: "",
			want:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeEndpoint(tc.input)
			if got != tc.want {
				t.Errorf("sanitizeEndpoint(%q)\n  got  %q\n  want %q", tc.input, got, tc.want)
			}
		})
	}
}
