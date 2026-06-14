package operator

import "strings"

// normalizeChainFilter returns "" for all-chains; otherwise lowercased chain id.
func normalizeChainFilter(chain string) string {
	c := strings.ToLower(strings.TrimSpace(chain))
	if c == "" || c == "all" {
		return ""
	}
	return c
}

func chainMatches(filter, value string) bool {
	if filter == "" {
		return true
	}
	return strings.EqualFold(filter, strings.TrimSpace(value))
}
