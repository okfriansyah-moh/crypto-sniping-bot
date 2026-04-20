package check

// DTO defines the data transfer objects for the health check feature.

// Response is the health check response DTO.
type Response struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}
