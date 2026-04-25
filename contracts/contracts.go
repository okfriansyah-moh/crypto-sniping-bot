package contracts

// contracts — Immutable DTO definitions.
// All inter-module communication flows through these types.
// DTOs are value objects — no methods that mutate state.
// This package is the ONLY coupling between modules.
//
// Canonical DTO registry (see docs/dto_contracts.md):
//   MarketDataDTO → DataQualityDTO → FeatureDTO → EdgeDTO →
//   ProbabilityEstimateDTO / SlippageEstimateDTO / LatencyProfileDTO →
//   ValidatedEdgeDTO → SelectionOutputDTO → AllocationDTO →
//   ExecutionResultDTO → PositionStateDTO → LearningRecordDTO / EvaluationDTO
