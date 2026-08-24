package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/model"
	"go-http-service/internal/service"
)

type stubRegistrar struct {
	user *model.User
	err  error
	ctx  context.Context
	in   service.RegisterInput
}

func (s *stubRegistrar) Register(ctx context.Context, in service.RegisterInput) (*model.User, error) {
	s.ctx = ctx
	s.in = in
	if s.err != nil {
		return nil, s.err
	}
	return s.user, nil
}

func registeredUser() *model.User {
	return &model.User{
		ID:           42,
		Username:     "jimmy",
		Email:        "jimmy@example.com",
		PasswordHash: "$2a$10$notarealhashbutlongenoughtolooklikeone000000000000000000",
		CreatedAt:    fixedTime,
		UpdatedAt:    fixedTime,
	}
}

const validUserBody = `{"username":"jimmy","email":"jimmy@example.com","password":"correct-horse"}`

func TestCreateUser_Success(t *testing.T) {
	t.Parallel()
	stub := &stubRegistrar{user: registeredUser()}
	w := request{
		method:  http.MethodPost,
		path:    "/api/users",
		body:    validUserBody,
		headers: authorHeaders(),
	}.doOn(t, routerWithRegistrar(t, stub))
	var got model.UserResponse
	requireJSONResponse(t, w, http.StatusCreated, &got)
	assert.Equal(t, int64(42), got.ID)
	assert.Equal(t, "jimmy", got.Username)
	assert.Equal(t, "jimmy@example.com", got.Email)
	assert.True(t, got.CreatedAt.Equal(fixedTime))
	assert.Equal(t, "jimmy", stub.in.Username)
	assert.Equal(t, "correct-horse", stub.in.Password)
}

func TestCreateUser_ResponseNeverContainsPasswordMaterial(t *testing.T) {
	t.Parallel()
	user := registeredUser()
	stub := &stubRegistrar{user: user}
	w := request{
		method:  http.MethodPost,
		path:    "/api/users",
		body:    validUserBody,
		headers: authorHeaders(),
	}.doOn(t, routerWithRegistrar(t, stub))
	require.Equal(t, http.StatusCreated, w.Code)
	body := w.Body.String()
	for _, leak := range []string{user.PasswordHash, "password_hash", "password", "correct-horse", "$2a$"} {
		assert.NotContains(t, body, leak)
	}
}

func TestCreateUser_PassesRequestContext(t *testing.T) {
	t.Parallel()
	stub := &stubRegistrar{user: registeredUser()}
	w := request{
		method:  http.MethodPost,
		path:    "/api/users",
		body:    validUserBody,
		headers: authorHeaders(),
	}.doOn(t, routerWithRegistrar(t, stub))
	require.Equal(t, http.StatusCreated, w.Code)
	require.NotNil(t, stub.ctx)
	_, hasDeadline := stub.ctx.Deadline()
	assert.True(t, hasDeadline)
	assert.NotEmpty(t, RequestIDFrom(stub.ctx))
}

func TestCreateUser_InternalErrorStaysOpaque(t *testing.T) {
	t.Parallel()
	const detail = "connection reset by peer at 10.0.0.5"
	stub := &stubRegistrar{err: errors.New(detail)}
	w := request{
		method:  http.MethodPost,
		path:    "/api/users",
		body:    validUserBody,
		headers: authorHeaders(),
	}.doOn(t, routerWithRegistrar(t, stub))
	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), detail)
	assert.NotContains(t, w.Body.String(), "10.0.0.5")
}

func TestCreateUser_ValidationErrorsNeverLeakInternals(t *testing.T) {
	t.Parallel()
	stub := &stubRegistrar{user: registeredUser()}
	w := request{
		method:  http.MethodPost,
		path:    "/api/users",
		body:    `{}`,
		headers: authorHeaders(),
	}.doOn(t, routerWithRegistrar(t, stub))
	body := w.Body.String()
	for _, leak := range []string{"CreateUserRequest", "Username", "Password", "struct", "Key:"} {
		assert.NotContains(t, body, leak)
	}
}
