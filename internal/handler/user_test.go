package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/model"
	"go-http-service/internal/service"
)

// stubRegistrar stands in for the user service so these tests exercise
// the HTTP contract without a database.
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

// registeredUser is what a successful Register returns, hash included -
// the handler is responsible for not passing that on.
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

// routerWithRegistrar builds a router whose only dependency is the stub.
func routerWithRegistrar(t *testing.T, stub *stubRegistrar) *gin.Engine {
	t.Helper()
	return SetupRouter(newTestAPI(WithUserService(stub)))
}

func TestCreateUser_Success(t *testing.T) {
	t.Parallel()

	stub := &stubRegistrar{user: registeredUser()}

	w := request{method: http.MethodPost, path: "/api/users", body: validUserBody}.
		doOn(t, routerWithRegistrar(t, stub))

	var got model.UserResponse
	requireJSONResponse(t, w, http.StatusCreated, &got)

	assert.Equal(t, int64(42), got.ID)
	assert.Equal(t, "jimmy", got.Username)
	assert.Equal(t, "jimmy@example.com", got.Email)
	assert.True(t, got.CreatedAt.Equal(fixedTime))

	// The request values reach the service unchanged.
	assert.Equal(t, "jimmy", stub.in.Username)
	assert.Equal(t, "correct-horse", stub.in.Password)
}

// TestCreateUser_ResponseNeverContainsPasswordMaterial is the HTTP-level
// counterpart to the model tests: even though Register hands back a User
// carrying a hash, none of it may reach the client.
func TestCreateUser_ResponseNeverContainsPasswordMaterial(t *testing.T) {
	t.Parallel()

	user := registeredUser()
	stub := &stubRegistrar{user: user}

	w := request{method: http.MethodPost, path: "/api/users", body: validUserBody}.
		doOn(t, routerWithRegistrar(t, stub))

	require.Equal(t, http.StatusCreated, w.Code)
	body := w.Body.String()

	for _, leak := range []string{
		user.PasswordHash, "password_hash", "password", "correct-horse", "$2a$",
	} {
		assert.NotContains(t, body, leak, "响应泄露了密码材料 %q: %s", leak, body)
	}
}

// TestCreateUser_PassesRequestContext confirms the deadline chain: the
// service must receive c.Request.Context(), not a fresh one, or
// REQUEST_TIMEOUT never reaches the query.
func TestCreateUser_PassesRequestContext(t *testing.T) {
	t.Parallel()

	stub := &stubRegistrar{user: registeredUser()}

	w := request{method: http.MethodPost, path: "/api/users", body: validUserBody}.
		doOn(t, routerWithRegistrar(t, stub))
	require.Equal(t, http.StatusCreated, w.Code)

	require.NotNil(t, stub.ctx)
	_, hasDeadline := stub.ctx.Deadline()
	assert.True(t, hasDeadline, "service 收到的 context 必须带请求超时")
	assert.NotEmpty(t, RequestIDFrom(stub.ctx), "context 应携带 request id")
}

func TestCreateUser_ServiceErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   model.ErrorCode
		wantField  string
	}{
		{
			name:       "用户名已被占用",
			err:        service.ErrUsernameTaken,
			wantStatus: http.StatusConflict,
			wantCode:   model.ErrCodeConflict,
		},
		{
			name:       "邮箱已被注册",
			err:        service.ErrEmailTaken,
			wantStatus: http.StatusConflict,
			wantCode:   model.ErrCodeConflict,
		},
		{
			name:       "密码超过字节上限",
			err:        service.ErrPasswordTooLong,
			wantStatus: http.StatusBadRequest,
			wantCode:   model.ErrCodeValidationFailed,
			wantField:  "password",
		},
		{
			name:       "未知错误",
			err:        errors.New("connection reset by peer"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   model.ErrCodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stub := &stubRegistrar{err: tt.err}

			w := request{method: http.MethodPost, path: "/api/users", body: validUserBody}.
				doOn(t, routerWithRegistrar(t, stub))

			var got model.ErrorResponse
			requireJSONResponse(t, w, tt.wantStatus, &got)

			assert.Equal(t, tt.wantCode, got.Code)
			assert.NotEmpty(t, got.Message)

			if tt.wantField != "" {
				require.Len(t, got.Fields, 1)
				assert.Equal(t, tt.wantField, got.Fields[0].Field)
			}
		})
	}
}

// TestCreateUser_InternalErrorStaysOpaque keeps the rule that a cause
// the client cannot act on is logged, not returned.
func TestCreateUser_InternalErrorStaysOpaque(t *testing.T) {
	t.Parallel()

	const detail = "connection reset by peer at 10.0.0.5"
	stub := &stubRegistrar{err: errors.New(detail)}

	w := request{method: http.MethodPost, path: "/api/users", body: validUserBody}.
		doOn(t, routerWithRegistrar(t, stub))

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), detail)
	assert.NotContains(t, w.Body.String(), "10.0.0.5")
}

func TestCreateUser_ValidationErrors(t *testing.T) {
	t.Parallel()

	longName := strings.Repeat("a", model.MaxUsernameLen+1)

	tests := []struct {
		name      string
		body      string
		wantField string
	}{
		{"缺少用户名", `{"email":"a@b.com","password":"longenough"}`, "username"},
		{"用户名过短", `{"username":"ab","email":"a@b.com","password":"longenough"}`, "username"},
		{"用户名过长", fmt.Sprintf(`{"username":%q,"email":"a@b.com","password":"longenough"}`, longName), "username"},
		{"用户名含非字母数字", `{"username":"jim my","email":"a@b.com","password":"longenough"}`, "username"},
		{"邮箱格式错误", `{"username":"jimmy","email":"not-an-email","password":"longenough"}`, "email"},
		{"缺少密码", `{"username":"jimmy","email":"a@b.com"}`, "password"},
		{"密码过短", `{"username":"jimmy","email":"a@b.com","password":"short"}`, "password"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stub := &stubRegistrar{user: registeredUser()}

			w := request{method: http.MethodPost, path: "/api/users", body: tt.body}.
				doOn(t, routerWithRegistrar(t, stub))

			var got model.ErrorResponse
			requireJSONResponse(t, w, http.StatusBadRequest, &got)

			assert.Equal(t, model.ErrCodeValidationFailed, got.Code)
			require.NotEmpty(t, got.Fields)
			assert.Equal(t, tt.wantField, got.Fields[0].Field)

			// Rejected input must never reach the service.
			assert.Nil(t, stub.ctx, "校验失败时不应调用 service")
		})
	}
}

// TestCreateUser_ValidationErrorsNeverLeakInternals extends the existing
// rule to the new endpoint.
func TestCreateUser_ValidationErrorsNeverLeakInternals(t *testing.T) {
	t.Parallel()

	stub := &stubRegistrar{user: registeredUser()}

	w := request{method: http.MethodPost, path: "/api/users", body: `{}`}.
		doOn(t, routerWithRegistrar(t, stub))

	body := w.Body.String()
	for _, leak := range []string{"CreateUserRequest", "Username", "Password", "struct", "Key:"} {
		assert.NotContains(t, body, leak, "响应泄露了 %q: %s", leak, body)
	}
}
