package model

import "time"

// Ready status values.
const (
	// StatusReady means every registered check passed.
	StatusReady = "ready"

	// StatusNotReady means at least one check failed.
	StatusNotReady = "not_ready"

	// CheckOK marks a passing individual check.
	CheckOK = "ok"

	// CheckFailed marks a failing individual check.
	CheckFailed = "failed"
)

// CheckResult reports one dependency probe.
type CheckResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	// DurationMS is how long the probe took, for spotting a dependency
	// that answers but has gone slow.
	DurationMS int64 `json:"duration_ms"`
	// Error carries the failure reason, omitted when the check passed.
	Error string `json:"error,omitempty"`
}

// ReadyResponse is the body of GET /api/ready.
//
// This is the one endpoint that does not use ErrorResponse on failure: it
// returns this same shape with HTTP 503 so the per-check detail survives.
// Readiness is consumed by an orchestrator probe rather than an API
// client, and knowing which dependency is down is the entire point.
type ReadyResponse struct {
	Status    string        `json:"status"`
	Timestamp time.Time     `json:"timestamp"`
	Checks    []CheckResult `json:"checks"`
}
