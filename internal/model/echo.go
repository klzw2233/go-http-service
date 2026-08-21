package model

import "time"

// MaxEchoMessageRunes caps the length of an echoed message. The body-size
// middleware bounds total memory; this bounds one field so a single
// oversized-but-legal string cannot be echoed back verbatim.
//
// Struct tags cannot reference constants, so the binding tag below
// repeats the number literally. A test asserts the two stay in sync.
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

// NewEchoResponse creates an EchoResponse stamped with the given time.
// The caller supplies the time so tests can pin it.
func NewEchoResponse(message string, at time.Time) EchoResponse {
	return EchoResponse{
		Message:  message,
		EchoedAt: at,
	}
}
