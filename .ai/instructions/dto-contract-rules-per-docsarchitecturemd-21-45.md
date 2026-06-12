The canonical DTO registry — no ad-hoc types allowed:

```
DataQualityDTO, FeatureDTO, FeatureConfidence, EdgeDTO,
ProbabilityEstimateDTO, SlippageEstimateDTO, LatencyProfileDTO,
ValidatedEdgeDTO, SelectionOutput, AllocationDTO,
ExecutionResultDTO, PositionState, LearningRecord,
StrategyConfig, StrategyVersion
```

All DTOs: immutable, versioned (`Version` field), schema-validated, `Timestamp` field required, no untyped payloads.

---