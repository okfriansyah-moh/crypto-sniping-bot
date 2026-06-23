package database_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOperatorCommandAuditMigration_AppendOnlyDDL(t *testing.T) {
	t.Parallel()
	path := filepath.Join("migrations", "20260613000001_operator_command_audit.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := strings.ToUpper(string(raw))

	if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS OPERATOR_COMMAND_AUDIT") {
		t.Fatal("migration must create operator_command_audit table")
	}
	if strings.Contains(sql, "UPDATE ") || strings.Contains(sql, "DELETE ") {
		t.Fatal("migration must be append-only DDL (no UPDATE/DELETE)")
	}
	if !strings.Contains(sql, "COMMAND_ID") || !strings.Contains(sql, "EVENT_ID") {
		t.Fatal("migration must define command_id and event_id columns")
	}
	if !strings.Contains(sql, "IDX_OPERATOR_COMMAND_AUDIT_TYPE_TIME") {
		t.Fatal("migration must index command_type + recorded_at")
	}
}
