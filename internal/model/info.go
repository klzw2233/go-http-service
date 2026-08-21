package model

import (
	"runtime"
	"time"
)

// Service identity. These are vars rather than consts so a release build
// can stamp real values in without editing source:
//
//	go build -ldflags "-X go-http-service/internal/model.Version=$(git describe --tags)"
//
// Previously Version was a literal inside the handler and a second
// literal inside its test, so bumping it meant editing two files.
var (
	// Name identifies this service.
	Name = "go-http-service"

	// Version is the service version. Overridden at build time.
	Version = "0.2.0"
)

// InfoResponse represents the response body of /api/info.
type InfoResponse struct {
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	GoVersion string    `json:"go_version"`
	Timestamp time.Time `json:"timestamp"`
}

// NewInfoResponse creates an InfoResponse describing this build, stamped
// with the given time. The caller supplies the time so tests can pin it.
func NewInfoResponse(at time.Time) InfoResponse {
	return InfoResponse{
		Name:      Name,
		Version:   Version,
		GoVersion: runtime.Version(),
		Timestamp: at,
	}
}
