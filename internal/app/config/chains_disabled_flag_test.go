package config_test

import (
	"testing"

	"gopkg.in/yaml.v3"

	"crypto-sniping-bot/internal/app/config"
)

// TestProgramConfig_DisabledFlagDefaultsFalse verifies that a SolanaProgramConfig
// parsed from YAML without a "disabled" key defaults to Disabled=false.
func TestProgramConfig_DisabledFlagDefaultsFalse(t *testing.T) {
	const input = `
program_id: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"
family: "pumpfun"
`
	var p config.SolanaProgramConfig
	if err := yaml.Unmarshal([]byte(input), &p); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if p.Disabled {
		t.Errorf("Disabled should default to false when absent from YAML, got true")
	}
}

// TestProgramConfig_DisabledFlagRespected verifies that "disabled: true" in YAML
// is parsed correctly and that "disabled: false" explicitly sets Disabled=false.
func TestProgramConfig_DisabledFlagRespected(t *testing.T) {
	cases := []struct {
		name         string
		yaml         string
		wantDisabled bool
	}{
		{
			name: "disabled_true",
			yaml: `
program_id: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"
family: "pumpfun"
disabled: true
`,
			wantDisabled: true,
		},
		{
			name: "disabled_false_explicit",
			yaml: `
program_id: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8"
family: "raydium-v4"
disabled: false
`,
			wantDisabled: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p config.SolanaProgramConfig
			if err := yaml.Unmarshal([]byte(tc.yaml), &p); err != nil {
				t.Fatalf("yaml.Unmarshal: %v", err)
			}
			if p.Disabled != tc.wantDisabled {
				t.Errorf("Disabled = %v, want %v", p.Disabled, tc.wantDisabled)
			}
		})
	}
}
