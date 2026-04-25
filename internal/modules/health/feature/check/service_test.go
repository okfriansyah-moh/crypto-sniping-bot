package check_test

import "testing"
import "crypto-sniping-bot/internal/modules/health/feature/check"

// ── Service.Execute ───────────────────────────────────────────────────────────

func TestService_Execute_StatusIsOK(t *testing.T) {
	// Arrange
	svc := check.NewService()

	// Act
	resp := svc.Execute()

	// Assert
	if resp.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", resp.Status)
	}
}

func TestService_Execute_VersionIsNonEmpty(t *testing.T) {
	// Arrange
	svc := check.NewService()

	// Act
	resp := svc.Execute()

	// Assert
	if resp.Version == "" {
		t.Error("expected non-empty Version")
	}
}

func TestService_Execute_Deterministic(t *testing.T) {
	// Same service instance must return the same values on repeated calls.
	// Arrange
	svc := check.NewService()

	// Act
	r1 := svc.Execute()
	r2 := svc.Execute()

	// Assert
	if r1.Status != r2.Status || r1.Version != r2.Version {
		t.Errorf("Execute is not deterministic: %+v vs %+v", r1, r2)
	}
}
