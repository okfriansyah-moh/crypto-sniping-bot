package workers

import (
	"encoding/json"
	"testing"

	"crypto-sniping-bot/shared/contracts"
)

// ── rejectionDecision ─────────────────────────────────────────────────────────

func TestRejectionDecision_DataQuality_Rejected(t *testing.T) {
	// Arrange
	dto := contracts.DataQualityDTO{
		TokenAddress:     "0xTOKEN",
		TokenLifecycleID: "lc-1",
		Decision:         "REJECT",
	}
	payload, _ := json.Marshal(dto)

	// Act
	tokenAddr, lcID, stage, isRejection := rejectionDecision("data_quality_event", payload)

	// Assert
	if !isRejection {
		t.Fatal("expected isRejection=true for REJECT decision")
	}
	if tokenAddr != "0xTOKEN" {
		t.Errorf("expected token 0xTOKEN, got %q", tokenAddr)
	}
	if lcID != "lc-1" {
		t.Errorf("expected lcID=lc-1, got %q", lcID)
	}
	if stage != "data_quality" {
		t.Errorf("expected stage=data_quality, got %q", stage)
	}
}

func TestRejectionDecision_DataQuality_Accepted_NotRejection(t *testing.T) {
	dto := contracts.DataQualityDTO{
		TokenAddress:     "0xTOKEN",
		TokenLifecycleID: "lc-1",
		Decision:         "PASS",
	}
	payload, _ := json.Marshal(dto)

	_, _, _, isRejection := rejectionDecision("data_quality_event", payload)
	if isRejection {
		t.Fatal("expected isRejection=false for PASS decision")
	}
}

func TestRejectionDecision_DataQuality_InvalidPayload_NotRejection(t *testing.T) {
	_, _, _, isRejection := rejectionDecision("data_quality_event", []byte("invalid json"))
	if isRejection {
		t.Fatal("expected isRejection=false for invalid payload")
	}
}

func TestRejectionDecision_Edge_LowStrength_IsRejection(t *testing.T) {
	dto := contracts.EdgeDTO{
		TokenAddress:     "0xEDGETOKEN",
		TokenLifecycleID: "lc-edge",
		EdgeStrength:     0.1, // below 0.3 threshold
	}
	payload, _ := json.Marshal(dto)

	tokenAddr, lcID, stage, isRejection := rejectionDecision("edge_event", payload)
	if !isRejection {
		t.Fatal("expected isRejection=true for low edge strength")
	}
	if tokenAddr != "0xEDGETOKEN" {
		t.Errorf("expected 0xEDGETOKEN, got %q", tokenAddr)
	}
	if lcID != "lc-edge" {
		t.Errorf("expected lc-edge, got %q", lcID)
	}
	if stage != "edge" {
		t.Errorf("expected edge, got %q", stage)
	}
}

func TestRejectionDecision_Edge_HighStrength_NotRejection(t *testing.T) {
	dto := contracts.EdgeDTO{
		TokenAddress: "0xTOKEN",
		EdgeStrength: 0.8, // above 0.3 threshold
	}
	payload, _ := json.Marshal(dto)

	_, _, _, isRejection := rejectionDecision("edge_event", payload)
	if isRejection {
		t.Fatal("expected isRejection=false for high edge strength")
	}
}

func TestRejectionDecision_ValidatedEdge_Rejected(t *testing.T) {
	dto := contracts.ValidatedEdgeDTO{
		TokenAddress:     "0xVAL",
		TokenLifecycleID: "lc-val",
		Decision:         "REJECT",
	}
	payload, _ := json.Marshal(dto)

	tokenAddr, lcID, stage, isRejection := rejectionDecision("validated_edge_event", payload)
	if !isRejection {
		t.Fatal("expected isRejection=true for REJECT validated edge")
	}
	if tokenAddr != "0xVAL" {
		t.Errorf("expected 0xVAL, got %q", tokenAddr)
	}
	if stage != "validated_edge" {
		t.Errorf("expected validated_edge, got %q", stage)
	}
	_ = lcID
}

func TestRejectionDecision_ValidatedEdge_Accepted_NotRejection(t *testing.T) {
	dto := contracts.ValidatedEdgeDTO{Decision: "ACCEPT"}
	payload, _ := json.Marshal(dto)

	_, _, _, isRejection := rejectionDecision("validated_edge_event", payload)
	if isRejection {
		t.Fatal("expected isRejection=false for ACCEPT")
	}
}

func TestRejectionDecision_Selection_NotSelected_IsRejection(t *testing.T) {
	dto := contracts.SelectionOutputDTO{
		TokenAddress:     "0xSEL",
		TokenLifecycleID: "lc-sel",
		Selected:         false,
	}
	payload, _ := json.Marshal(dto)

	tokenAddr, _, stage, isRejection := rejectionDecision("selection_event", payload)
	if !isRejection {
		t.Fatal("expected isRejection=true for not-selected")
	}
	if tokenAddr != "0xSEL" {
		t.Errorf("expected 0xSEL, got %q", tokenAddr)
	}
	if stage != "selection" {
		t.Errorf("expected selection, got %q", stage)
	}
}

func TestRejectionDecision_Selection_Selected_NotRejection(t *testing.T) {
	dto := contracts.SelectionOutputDTO{Selected: true}
	payload, _ := json.Marshal(dto)

	_, _, _, isRejection := rejectionDecision("selection_event", payload)
	if isRejection {
		t.Fatal("expected isRejection=false for selected token")
	}
}

func TestRejectionDecision_UnknownEventType_NotRejection(t *testing.T) {
	_, _, _, isRejection := rejectionDecision("unknown_event", []byte(`{}`))
	if isRejection {
		t.Fatal("expected isRejection=false for unknown event type")
	}
}
