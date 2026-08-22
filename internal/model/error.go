package model

// ErrorCode is a stable, machine-readable identifier for a failure.
// Clients should switch on this value; Message is meant for humans and
// its wording may change without notice.
type ErrorCode string

const (
	// ErrCodeInvalidJSON means the body could not be parsed as JSON.
	ErrCodeInvalidJSON ErrorCode = "INVALID_JSON"

	// ErrCodeValidationFailed means the body parsed but one or more
	// fields were missing, malformed, or the wrong type.
	ErrCodeValidationFailed ErrorCode = "VALIDATION_FAILED"

	// ErrCodePayloadTooLarge means the body exceeded the size limit.
	ErrCodePayloadTooLarge ErrorCode = "PAYLOAD_TOO_LARGE"

	// ErrCodeNotFound means no route matched the request path.
	ErrCodeNotFound ErrorCode = "NOT_FOUND"

	// ErrCodeMethodNotAllowed means the path exists but not for this
	// HTTP method.
	ErrCodeMethodNotAllowed ErrorCode = "METHOD_NOT_ALLOWED"

	// ErrCodeConflict means the request collided with existing state,
	// such as registering a username somebody already holds.
	ErrCodeConflict ErrorCode = "CONFLICT"

	// ErrCodeUnauthorized means the request carried no usable
	// credentials. It deliberately does not distinguish a missing token
	// from an expired or forged one: telling a caller which of those
	// applies tells an attacker how close they are.
	ErrCodeUnauthorized ErrorCode = "UNAUTHORIZED"

	// ErrCodeRateLimited means the caller exceeded its request budget.
	ErrCodeRateLimited ErrorCode = "RATE_LIMITED"

	// ErrCodeInternal means the request failed for a reason the client
	// cannot act on. Details go to the server log only.
	ErrCodeInternal ErrorCode = "INTERNAL_ERROR"
)

// FieldError describes why one specific field was rejected. Field holds
// the JSON name as it appears on the wire, never the Go struct field
// name, so responses do not expose internal type layout.
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// ErrorResponse is the single error shape returned by every endpoint.
// Success responses are typed models, so errors are too; this replaces
// the ad-hoc gin.H maps that produced a different shape per call site.
type ErrorResponse struct {
	Code    ErrorCode    `json:"code"`
	Message string       `json:"message"`
	Fields  []FieldError `json:"fields,omitempty"`
}

// NewErrorResponse builds an ErrorResponse with no field detail.
func NewErrorResponse(code ErrorCode, message string) ErrorResponse {
	return ErrorResponse{Code: code, Message: message}
}
