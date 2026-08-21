package model

import "time"

// EchoRequest represents the request body of /api/echo.
type EchoRequest struct {
	Message string `json:"message" binding:"required"`
}

// EchoResponse represents the response body of /api/echo.
type EchoResponse struct {
	Message  string    `json:"message"`
	EchoedAt time.Time `json:"echoed_at"`
}
