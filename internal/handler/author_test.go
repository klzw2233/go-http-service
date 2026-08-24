package handler

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/model"
)

const authorBearer = "Bearer " + testAccessToken

func authorHeaders() map[string]string {
	return map[string]string{"Authorization": authorBearer}
}

// routerWithRegistrar wires the stubs a successful Author registration needs.
func routerWithRegistrar(t *testing.T, stub *stubRegistrar) *gin.Engine {
	t.Helper()
	return SetupRouter(newTestAPI(
		WithUserService(stub),
		WithAuthService(&stubAuthenticator{user: registeredUser()}),
		WithTokenVerifier(&stubVerifier{userID: registeredUser().ID}),
	))
}

func TestCreateUser_AnonymousForbidden(t *testing.T) {
	t.Parallel()

	stub := &stubRegistrar{user: registeredUser()}
	w := request{method: http.MethodPost, path: "/api/users", body: validUserBody}.
		doOn(t, routerWithRegistrar(t, stub))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusForbidden, &got)
	assert.Equal(t, model.ErrCodeForbidden, got.Code)
	assert.Empty(t, w.Header().Get("WWW-Authenticate"))
	assert.Nil(t, stub.ctx, "anonymous registration must not reach the service")
}

func TestCreateUser_NonAuthorForbidden(t *testing.T) {
	t.Parallel()

	stub := &stubRegistrar{user: registeredUser()}
	other := registeredUser()
	other.Username = "notjimmy"

	w := request{
		method:  http.MethodPost,
		path:    "/api/users",
		body:    validUserBody,
		headers: authorHeaders(),
	}.doOn(t, SetupRouter(newTestAPI(
		WithUserService(stub),
		WithAuthService(&stubAuthenticator{user: other}),
		WithTokenVerifier(&stubVerifier{userID: other.ID}),
	)))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusForbidden, &got)
	assert.Equal(t, model.ErrCodeForbidden, got.Code)
	assert.Nil(t, stub.ctx)
}

func TestCreateUser_BadTokenUnauthorized(t *testing.T) {
	t.Parallel()

	stub := &stubRegistrar{user: registeredUser()}
	w := request{
		method:  http.MethodPost,
		path:    "/api/users",
		body:    validUserBody,
		headers: authorHeaders(),
	}.doOn(t, SetupRouter(newTestAPI(
		WithUserService(stub),
		WithAuthService(&stubAuthenticator{user: registeredUser()}),
		WithTokenVerifier(&stubVerifier{err: assert.AnError}),
	)))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusUnauthorized, &got)
	assert.Equal(t, model.ErrCodeUnauthorized, got.Code)
	assert.Equal(t, `Bearer realm="api"`, w.Header().Get("WWW-Authenticate"))
}

func TestCreateUser_AuthorCaseInsensitive(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.AuthorUsername = "Jimmy"
	stub := &stubRegistrar{user: registeredUser()}

	w := request{
		method:  http.MethodPost,
		path:    "/api/users",
		body:    validUserBody,
		headers: authorHeaders(),
	}.doOn(t, SetupRouter(newTestAPIWith(cfg, discardLogger(),
		WithUserService(stub),
		WithAuthService(&stubAuthenticator{user: registeredUser()}),
		WithTokenVerifier(&stubVerifier{userID: registeredUser().ID}),
	)))

	require.Equal(t, http.StatusCreated, w.Code)
}
