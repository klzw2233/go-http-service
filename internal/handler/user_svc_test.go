package handler

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/model"
	"go-http-service/internal/service"
)

func TestCreateUser_UsernameTaken(t *testing.T) {
	t.Parallel()
	stub := &stubRegistrar{err: service.ErrUsernameTaken}
	w := request{
		method:  http.MethodPost,
		path:    "/api/users",
		body:    validUserBody,
		headers: authorHeaders(),
	}.doOn(t, routerWithRegistrar(t, stub))
	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusConflict, &got)
	assert.Equal(t, model.ErrCodeConflict, got.Code)
}

func TestCreateUser_EmailTaken(t *testing.T) {
	t.Parallel()
	stub := &stubRegistrar{err: service.ErrEmailTaken}
	w := request{
		method:  http.MethodPost,
		path:    "/api/users",
		body:    validUserBody,
		headers: authorHeaders(),
	}.doOn(t, routerWithRegistrar(t, stub))
	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusConflict, &got)
	assert.Equal(t, model.ErrCodeConflict, got.Code)
}

func TestCreateUser_PasswordTooLong(t *testing.T) {
	t.Parallel()
	stub := &stubRegistrar{err: service.ErrPasswordTooLong}
	w := request{
		method:  http.MethodPost,
		path:    "/api/users",
		body:    validUserBody,
		headers: authorHeaders(),
	}.doOn(t, routerWithRegistrar(t, stub))
	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusBadRequest, &got)
	assert.Equal(t, model.ErrCodeValidationFailed, got.Code)
	require.Len(t, got.Fields, 1)
	assert.Equal(t, "password", got.Fields[0].Field)
}

func TestCreateUser_UnknownServiceError(t *testing.T) {
	t.Parallel()
	stub := &stubRegistrar{err: errors.New("connection reset by peer")}
	w := request{
		method:  http.MethodPost,
		path:    "/api/users",
		body:    validUserBody,
		headers: authorHeaders(),
	}.doOn(t, routerWithRegistrar(t, stub))
	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusInternalServerError, &got)
	assert.Equal(t, model.ErrCodeInternal, got.Code)
}
