package model

import "time"

// MaxEchoMessageRunes caps the length of an echoed message. The body-size
// middleware bounds total memory; this bounds one field so a single
// oversized-but-legal string cannot be echoed back verbatim.
const MaxEchoMessageRunes = 4096

// EchoRequest represents the request body of /api/echo.
type EchoRequest struct {
	Message string `json:"message" binding:"required,max=4096"`
}

// EchoResponse represents the response body of /api/echo.
type EchoResponse struct {
	Message  string    `json:"message"`
	EchoedAt time.Time `json:"echoed_at"`
}
