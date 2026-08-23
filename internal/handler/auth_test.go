package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/model"
	"go-http-service/internal/service"
)

const (
	testAccessToken = "header.payload.signature"
	testUserID      = int64(42)
)

// stubAuthenticator stands in for the auth service.
type stubAuthenticator struct {
	result *service.LoginResult
	user   *model.User
	err    error

	ctx      context.Context
	username string
	password string
	lookedUp int64
}

func (s *stubAuthenticator) Login(ctx context.Context, username, password string) (*service.LoginResult, error) {
	s.ctx = ctx
	s.username, s.password = username, password
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func (s *stubAuthenticator) UserByID(ctx context.Context, id int64) (*model.User, error) {
	s.ctx = ctx
	s.lookedUp = id
	if s.err != nil {
		return nil, s.err
	}
	return s.user, nil
}

// stubVerifier stands in for the token issuer's verification half.
type stubVerifier struct {
	userID int64
	err    error
	seen   string
}

func (s *stubVerifier) ParseAccess(token string) (int64, error) {
	s.seen = token
	if s.err != nil {
		return 0, s.err
	}
	return s.userID, nil
}

func authedUser() *model.User {
	return &model.User{
		ID:           testUserID,
		Username:     "jimmy",
		Email:        "jimmy@example.com",
		PasswordHash: "$2a$10$notarealhashbutlongenoughtolooklikeone00000000000000000",
		CreatedAt:    fixedTime,
		UpdatedAt:    fixedTime,
	}
}

func loginResult() *service.LoginResult {
	return &service.LoginResult{
		User:        authedUser(),
		AccessToken: testAccessToken,
		ExpiresAt:   fixedTime.Add(15 * time.Minute),
	}
}

// authRouter builds a router wired to the given stubs.
func authRouter(t *testing.T, a *stubAuthenticator, v *stubVerifier) *gin.Engine {
	t.Helper()
	return SetupRouter(newTestAPI(WithAuthService(a), WithTokenVerifier(v)))
}

const validLoginBody = `{"username":"jimmy","password":"correct-horse"}`

func TestLogin_Succeeds(t *testing.T) {
	t.Parallel()

	stub := &stubAuthenticator{result: loginResult()}

	w := request{method: http.MethodPost, path: "/api/auth/login", body: validLoginBody}.
		doOn(t, authRouter(t, stub, &stubVerifier{}))

	var got model.TokenPair
	requireJSONResponse(t, w, http.StatusOK, &got)

	assert.Equal(t, testAccessToken, got.AccessToken)
	assert.Equal(t, "Bearer", got.TokenType)
	assert.True(t, got.ExpiresAt.Equal(fixedTime.Add(15*time.Minute)))

	assert.Equal(t, "jimmy", stub.username)
	assert.Equal(t, "correct-horse", stub.password)
}

// TestLogin_ResponseCarriesNoUserData keeps the login response to a
// credential. Anything else about the account belongs behind the token.
func TestLogin_ResponseCarriesNoUserData(t *testing.T) {
	t.Parallel()

	w := request{method: http.MethodPost, path: "/api/auth/login", body: validLoginBody}.
		doOn(t, authRouter(t, &stubAuthenticator{result: loginResult()}, &stubVerifier{}))

	body := w.Body.String()
	for _, leak := range []string{"password", "$2a$", "jimmy@example.com", "email"} {
		assert.NotContains(t, body, leak, "登录响应泄露了 %q: %s", leak, body)
	}
}

func TestLogin_InvalidCredentials(t *testing.T) {
	t.Parallel()

	stub := &stubAuthenticator{err: service.ErrInvalidCredentials}

	w := request{method: http.MethodPost, path: "/api/auth/login", body: validLoginBody}.
		doOn(t, authRouter(t, stub, &stubVerifier{}))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusUnauthorized, &got)

	assert.Equal(t, model.ErrCodeUnauthorized, got.Code)
	assert.Equal(t, `Bearer realm="api"`, w.Header().Get("WWW-Authenticate"),
		"RFC 7235 要求 401 带 WWW-Authenticate")
}

func TestLogin_InternalErrorStaysOpaque(t *testing.T) {
	t.Parallel()

	const detail = "connection reset by peer at 10.0.0.5"
	stub := &stubAuthenticator{err: errors.New(detail)}

	w := request{method: http.MethodPost, path: "/api/auth/login", body: validLoginBody}.
		doOn(t, authRouter(t, stub, &stubVerifier{}))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusInternalServerError, &got)

	assert.Equal(t, model.ErrCodeInternal, got.Code)
	assert.NotContains(t, w.Body.String(), detail)
	assert.NotContains(t, w.Body.String(), "10.0.0.5")
}

func TestLogin_PassesRequestContext(t *testing.T) {
	t.Parallel()

	stub := &stubAuthenticator{result: loginResult()}

	request{method: http.MethodPost, path: "/api/auth/login", body: validLoginBody}.
		doOn(t, authRouter(t, stub, &stubVerifier{}))

	require.NotNil(t, stub.ctx)
	_, hasDeadline := stub.ctx.Deadline()
	assert.True(t, hasDeadline, "service 收到的 context 必须带请求超时")
}

func TestMe_Succeeds(t *testing.T) {
	t.Parallel()

	stub := &stubAuthenticator{user: authedUser()}
	verifier := &stubVerifier{userID: testUserID}

	w := request{
		method:  http.MethodGet,
		path:    "/api/auth/me",
		headers: map[string]string{"Authorization": "Bearer " + testAccessToken},
	}.doOn(t, authRouter(t, stub, verifier))

	var got model.UserResponse
	requireJSONResponse(t, w, http.StatusOK, &got)

	assert.Equal(t, testUserID, got.ID)
	assert.Equal(t, "jimmy", got.Username)

	assert.Equal(t, testAccessToken, verifier.seen, "中间件应把 Bearer 之后的部分交给验证器")
	assert.Equal(t, testUserID, stub.lookedUp, "应按 token 里的 subject 查用户")
}

// TestMe_ResponseOmitsPasswordHash extends the projection rule to the
// authenticated read.
func TestMe_ResponseOmitsPasswordHash(t *testing.T) {
	t.Parallel()

	user := authedUser()

	w := request{
		method:  http.MethodGet,
		path:    "/api/auth/me",
		headers: map[string]string{"Authorization": "Bearer " + testAccessToken},
	}.doOn(t, authRouter(t, &stubAuthenticator{user: user}, &stubVerifier{userID: testUserID}))

	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), user.PasswordHash)
	assert.NotContains(t, w.Body.String(), "password")
}

// TestRequireAuth_RejectsUniformly is the middleware's central rule.
//
// Missing, malformed, wrong scheme, expired and forged must be
// indistinguishable. "Expired" would confirm the signature verified,
// which tells an attacker they have a real token and only need a fresh
// one - very different news from "malformed".
func TestRequireAuth_RejectsUniformly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		header   string
		parseErr error
	}{
		{name: "没有 Authorization 头"},
		{name: "空的 Authorization 头", header: ""},
		{name: "只有 scheme", header: "Bearer"},
		{name: "scheme 后为空", header: "Bearer   "},
		{name: "scheme 不对", header: "Basic " + testAccessToken},
		{name: "没有 scheme", header: testAccessToken},
		{name: "token 已过期", header: "Bearer " + testAccessToken, parseErr: errors.New("token is expired")},
		{name: "签名无效", header: "Bearer " + testAccessToken, parseErr: errors.New("signature invalid")},
	}

	var bodies []string

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{}
			if tt.header != "" {
				headers["Authorization"] = tt.header
			}

			w := request{method: http.MethodGet, path: "/api/auth/me", headers: headers}.
				doOn(t, authRouter(t,
					&stubAuthenticator{user: authedUser()},
					&stubVerifier{userID: testUserID, err: tt.parseErr}))

			var got model.ErrorResponse
			requireJSONResponse(t, w, http.StatusUnauthorized, &got)

			assert.Equal(t, model.ErrCodeUnauthorized, got.Code)
			assert.Equal(t, `Bearer realm="api"`, w.Header().Get("WWW-Authenticate"))

			bodies = append(bodies, w.Body.String())
		})
	}

	for i := 1; i < len(bodies); i++ {
		assert.Equal(t, bodies[0], bodies[i],
			"所有认证失败的响应体必须完全一致，第 %d 个不同", i)
	}
}

// TestRequireAuth_AcceptsSchemeCaseInsensitively follows RFC 7235,
// where the scheme token is case-insensitive. Clients do send "bearer".
func TestRequireAuth_AcceptsSchemeCaseInsensitively(t *testing.T) {
	t.Parallel()

	for _, scheme := range []string{"Bearer", "bearer", "BEARER", "BeArEr"} {
		t.Run(scheme, func(t *testing.T) {
			t.Parallel()

			w := request{
				method:  http.MethodGet,
				path:    "/api/auth/me",
				headers: map[string]string{"Authorization": scheme + " " + testAccessToken},
			}.doOn(t, authRouter(t,
				&stubAuthenticator{user: authedUser()},
				&stubVerifier{userID: testUserID}))

			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

// TestRequireAuth_DeletedAccountIsUnauthorized covers a token that
// verified but whose owner no longer exists.
func TestRequireAuth_DeletedAccountIsUnauthorized(t *testing.T) {
	t.Parallel()

	w := request{
		method:  http.MethodGet,
		path:    "/api/auth/me",
		headers: map[string]string{"Authorization": "Bearer " + testAccessToken},
	}.doOn(t, authRouter(t,
		&stubAuthenticator{err: service.ErrInvalidCredentials},
		&stubVerifier{userID: testUserID}))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusUnauthorized, &got)
	assert.Equal(t, model.ErrCodeUnauthorized, got.Code)
}

// TestRequireAuth_DoesNotCallServiceWhenTokenInvalid keeps a rejected
// request from reaching the database at all - otherwise an attacker
// could drive load with junk tokens.
func TestRequireAuth_DoesNotCallServiceWhenTokenInvalid(t *testing.T) {
	t.Parallel()

	stub := &stubAuthenticator{user: authedUser()}

	request{
		method:  http.MethodGet,
		path:    "/api/auth/me",
		headers: map[string]string{"Authorization": "Bearer bad"},
	}.doOn(t, authRouter(t, stub, &stubVerifier{err: errors.New("nope")}))

	assert.Nil(t, stub.ctx, "token 无效时不应查询数据库")
}

func TestBearerToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		header    string
		want      string
		wantValid bool
	}{
		{header: "Bearer abc", want: "abc", wantValid: true},
		{header: "bearer abc", want: "abc", wantValid: true},
		{header: "Bearer   abc  ", want: "abc", wantValid: true},
		{header: "", wantValid: false},
		{header: "Bearer", wantValid: false},
		{header: "Bearer ", wantValid: false},
		{header: "Basic abc", wantValid: false},
		{header: "abc", wantValid: false},
		{header: strings.Repeat("x", 100), wantValid: false},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			t.Parallel()

			got, ok := bearerToken(tt.header)

			assert.Equal(t, tt.wantValid, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUserIDFrom_EmptyContext(t *testing.T) {
	t.Parallel()

	_, ok := UserIDFrom(t.Context())
	assert.False(t, ok)
}
