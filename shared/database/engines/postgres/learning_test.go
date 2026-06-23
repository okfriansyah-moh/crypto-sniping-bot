package postgres

import (
	"strings"
	"testing"
)

func TestGetShadowTradesByWindowSQL_UsesMakeInterval(t *testing.T) {
	t.Parallel()
	if strings.Contains(getShadowTradesByWindowSQL, "|| ' seconds'") {
		t.Fatal("getShadowTradesByWindowSQL must not use string concat on $1 — pgx cannot encode int as text")
	}
	if !strings.Contains(getShadowTradesByWindowSQL, "make_interval(secs => $1::double precision)") {
		t.Fatal("getShadowTradesByWindowSQL must use make_interval for windowSeconds binding")
	}
}

func TestGetShadowTradesByWindowSQL_FiltersPendingOnly(t *testing.T) {
	t.Parallel()
	if !strings.Contains(getShadowTradesByWindowSQL, "observation_complete = FALSE") {
		t.Fatal("getShadowTradesByWindowSQL must filter observation_complete = FALSE")
	}
}
