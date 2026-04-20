package port

// HealthPort defines the public interface for the health module.
// Other modules depend on this interface, not on internal implementations.
type HealthPort interface {
	Check() HealthStatus
}

// HealthStatus is the result of a health check.
type HealthStatus struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}
