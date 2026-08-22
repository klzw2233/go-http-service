package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/model"
)

// errCheckFailed deliberately carries the kind of detail a real driver
// error would: a host and a username. Nothing in it may reach a client.
var errCheckFailed = errors.New("dial 10.0.0.5:5432 refused, user=admin password=hunter2")

func failingCheck(context.Context) error { return errCheckFailed }

func passingCheck(context.Context) error { return nil }

func TestReady_NoChecksRegistered(t *testing.T) {
	t.Parallel()

	w := request{method: http.MethodGet, path: "/api/ready"}.do(t)

	var got model.ReadyResponse
	requireJSONResponse(t, w, http.StatusOK, &got)

	assert.Equal(t, model.StatusReady, got.Status)
	assert.Empty(t, got.Checks, "没有依赖时应为空列表")
	assert.True(t, got.Timestamp.Equal(fixedTime))
	// A nil slice would marshal to null; probe consumers expect an array.
	assert.Contains(t, w.Body.String(), `"checks":[]`)
}

func TestReady_AllChecksPass(t *testing.T) {
	t.Parallel()

	api := newTestAPI(
		WithReadyCheck("database", passingCheck),
		WithReadyCheck("cache", passingCheck),
	)
	w := request{method: http.MethodGet, path: "/api/ready"}.doOn(t, SetupRouter(api))

	var got model.ReadyResponse
	requireJSONResponse(t, w, http.StatusOK, &got)

	assert.Equal(t, model.StatusReady, got.Status)
	require.Len(t, got.Checks, 2)

	// Registration order is preserved regardless of completion order.
	assert.Equal(t, "database", got.Checks[0].Name)
	assert.Equal(t, "cache", got.Checks[1].Name)
	for _, c := range got.Checks {
		assert.Equal(t, model.CheckOK, c.Status)
		assert.Empty(t, c.Error)
	}
}

func TestReady_OneCheckFailsMeans503(t *testing.T) {
	t.Parallel()

	api := newTestAPI(
		WithReadyCheck("database", failingCheck),
		WithReadyCheck("cache", passingCheck),
	)
	w := request{method: http.MethodGet, path: "/api/ready"}.doOn(t, SetupRouter(api))

	var got model.ReadyResponse
	requireJSONResponse(t, w, http.StatusServiceUnavailable, &got)

	assert.Equal(t, model.StatusNotReady, got.Status)
	require.Len(t, got.Checks, 2)

	assert.Equal(t, "database", got.Checks[0].Name)
	assert.Equal(t, model.CheckFailed, got.Checks[0].Status)
	assert.Equal(t, reasonFailed, got.Checks[0].Error)

	assert.Equal(t, "cache", got.Checks[1].Name)
	assert.Equal(t, model.CheckOK, got.Checks[1].Status)
}

// TestReady_NeverLeaksCheckError is the readiness counterpart of the
// binding-error rule: a driver error can carry a host, a user, even a
// password, and none of it belongs in a response.
func TestReady_NeverLeaksCheckError(t *testing.T) {
	t.Parallel()

	api := newTestAPI(WithReadyCheck("database", failingCheck))
	w := request{method: http.MethodGet, path: "/api/ready"}.doOn(t, SetupRouter(api))

	body := w.Body.String()
	require.Equal(t, http.StatusServiceUnavailable, w.Code)

	for _, leak := range []string{"10.0.0.5", "admin", "hunter2", "refused", "dial"} {
		assert.NotContains(t, body, leak, "readiness 响应泄露了 %q: %s", leak, body)
	}
	// The dependency name is still reported: that is the actionable part.
	assert.Contains(t, body, "database")
}

func TestReady_TimedOutCheckIsDistinguishable(t *testing.T) {
	t.Parallel()

	api := newTestAPI(WithReadyCheck("slow", func(context.Context) error {
		return context.DeadlineExceeded
	}))
	w := request{method: http.MethodGet, path: "/api/ready"}.doOn(t, SetupRouter(api))

	var got model.ReadyResponse
	requireJSONResponse(t, w, http.StatusServiceUnavailable, &got)

	require.Len(t, got.Checks, 1)
	assert.Equal(t, reasonTimedOut, got.Checks[0].Error,
		"超时和普通失败应该可区分，否则排查时分不清是慢还是断")
}

// TestReady_PanickingCheckDoesNotCrash covers the recover in
// runReadyCheck. A probe is exactly the code that panics on a nil client
// during startup, and that must not take the process down.
func TestReady_PanickingCheckDoesNotCrash(t *testing.T) {
	t.Parallel()

	api := newTestAPI(
		WithReadyCheck("panicky", func(context.Context) error {
			panic("nil pool dereferenced")
		}),
		WithReadyCheck("healthy", passingCheck),
	)
	w := request{method: http.MethodGet, path: "/api/ready"}.doOn(t, SetupRouter(api))

	var got model.ReadyResponse
	requireJSONResponse(t, w, http.StatusServiceUnavailable, &got)

	require.Len(t, got.Checks, 2)
	assert.Equal(t, model.CheckFailed, got.Checks[0].Status)
	assert.Equal(t, reasonPanicked, got.Checks[0].Error)
	assert.NotContains(t, w.Body.String(), "nil pool dereferenced")

	// The other check still ran and reported.
	assert.Equal(t, model.CheckOK, got.Checks[1].Status)
}

// TestReady_ChecksRunConcurrently guards the goroutine fan-out. Run
// serially, three 60ms probes would take at least 180ms.
func TestReady_ChecksRunConcurrently(t *testing.T) {
	t.Parallel()

	const probe = 60 * time.Millisecond

	slow := func(context.Context) error {
		time.Sleep(probe)
		return nil
	}

	api := newTestAPI(
		WithReadyCheck("a", slow),
		WithReadyCheck("b", slow),
		WithReadyCheck("c", slow),
	)
	router := SetupRouter(api)

	start := time.Now()
	w := request{method: http.MethodGet, path: "/api/ready"}.doOn(t, router)
	elapsed := time.Since(start)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Less(t, elapsed, 3*probe,
		"三个 %s 的检查耗时 %s，看起来是串行执行的", probe, elapsed)
}

// TestReady_CheckReceivesRequestContext confirms probes inherit the
// request deadline, so a hung dependency cannot outlive its request.
func TestReady_CheckReceivesRequestContext(t *testing.T) {
	t.Parallel()

	gotDeadline := make(chan bool, 1)

	api := newTestAPI(WithReadyCheck("deadline", func(ctx context.Context) error {
		_, ok := ctx.Deadline()
		gotDeadline <- ok
		return nil
	}))
	w := request{method: http.MethodGet, path: "/api/ready"}.doOn(t, SetupRouter(api))

	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, <-gotDeadline, "检查函数拿到的 context 应带有超时")
}
