package model

import "time"

// HealthResponse represents the response body of /api/health.
type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// StatusOK is the Status value of a healthy service.
const StatusOK = "ok"

// NewHealthResponse creates a HealthResponse stamped with the given time.
// The caller supplies the time so tests can pin it.
func NewHealthResponse(at time.Time) HealthResponse {
	return HealthResponse{
		Status:    StatusOK,
		Timestamp: at,
	}
}
