package model

import "time"

// HealthResponse represents the response body of /api/health.
type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// NewHealthResponse creates a new HealthResponse with the current UTC time.
func NewHealthResponse() HealthResponse {
	return HealthResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC(),
	}
}

// InfoResponse represents the response body of /api/info.
type InfoResponse struct {
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	GoVersion string    `json:"go_version"`
	Timestamp time.Time `json:"timestamp"`
}
