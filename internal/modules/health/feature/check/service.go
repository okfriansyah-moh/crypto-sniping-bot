package check

// Service implements the health check business logic.
type Service struct{}

// NewService creates a new health check service.
func NewService() *Service {
	return &Service{}
}

// Execute performs the health check.
func (s *Service) Execute() Response {
	return Response{
		Status:  "ok",
		Version: "1.0.0",
	}
}
