package handler

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/model"
)

// Generic failure reasons reported to the caller.
//
// The underlying error is logged but never returned: a dependency error
// can carry a connection string, a host name, or credentials, and the
// same rule that keeps validator internals out of responses applies here.
// The check name already says which dependency is unhappy, which is the
// actionable part; the reason distinguishes a timeout from a hard failure.
const (
	reasonFailed   = "check failed"
	reasonTimedOut = "check timed out"
	reasonPanicked = "check panicked"
)

// Ready handles GET /api/ready, the readiness probe.
//
// Unlike Health it does probe dependencies: it answers whether the
// service can serve traffic right now. An orchestrator uses it to decide
// whether to route requests here, without restarting the process the way
// a failing liveness probe would.
//
// With no checks registered this returns 200 and an empty list, which is
// the correct answer for a service that has no dependencies yet.
func (a *API) Ready(c *gin.Context) {
	results := a.runReadyChecks(c.Request.Context())

	status, code := model.StatusReady, http.StatusOK
	for _, r := range results {
		if r.Status != model.CheckOK {
			status, code = model.StatusNotReady, http.StatusServiceUnavailable
			break
		}
	}

	c.JSON(code, model.ReadyResponse{
		Status:    status,
		Timestamp: a.now(),
		Checks:    results,
	})
}

// runReadyChecks probes every dependency concurrently so a slow check
// does not queue behind another. Results are written by index, keeping
// registration order regardless of completion order so the response is
// stable and diffable.
func (a *API) runReadyChecks(ctx context.Context) []model.CheckResult {
	results := make([]model.CheckResult, len(a.readyChecks))

	var wg sync.WaitGroup
	for i, check := range a.readyChecks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = a.runReadyCheck(ctx, check)
		}()
	}
	wg.Wait()

	return results
}

// runReadyCheck runs one probe and converts a panic into a failed check.
// A dependency probe is exactly the code that panics on a nil client
// during startup, and that must not take the process down.
func (a *API) runReadyCheck(ctx context.Context, check readyCheck) (result model.CheckResult) {
	start := a.now()
	result = model.CheckResult{Name: check.name, Status: model.CheckOK}

	defer func() {
		if r := recover(); r != nil {
			a.log.Error("readiness check panicked",
				"check", check.name, "panic", r)
			result = model.CheckResult{
				Name:   check.name,
				Status: model.CheckFailed,
				Error:  reasonPanicked,
			}
		}
		result.DurationMS = a.now().Sub(start).Milliseconds()
	}()

	if err := check.fn(ctx); err != nil {
		a.log.Error("readiness check failed", "check", check.name, "error", err)

		reason := reasonFailed
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			reason = reasonTimedOut
		}
		return model.CheckResult{
			Name:   check.name,
			Status: model.CheckFailed,
			Error:  reason,
		}
	}

	return result
}
