package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/model"
)

func TestCreateUser_MissingUsername(t *testing.T) {
	t.Parallel()
	assertValidationField(t, `{"email":"a@b.com","password":"longenough"}`, "username")
}

func TestCreateUser_UsernameShort(t *testing.T) {
	t.Parallel()
	assertValidationField(t, `{"username":"ab","email":"a@b.com","password":"longenough"}`, "username")
}

func TestCreateUser_UsernameLong(t *testing.T) {
	t.Parallel()
	longName := strings.Repeat("a", model.MaxUsernameLen+1)
	body := fmt.Sprintf(`{"username":%q,"email":"a@b.com","password":"longenough"}`, longName)
	assertValidationField(t, body, "username")
}

func TestCreateUser_UsernameNotAlphanum(t *testing.T) {
	t.Parallel()
	assertValidationField(t, `{"username":"jim my","email":"a@b.com","password":"longenough"}`, "username")
}

func TestCreateUser_BadEmail(t *testing.T) {
	t.Parallel()
	assertValidationField(t, `{"username":"jimmy","email":"not-an-email","password":"longenough"}`, "email")
}

func TestCreateUser_MissingPassword(t *testing.T) {
	t.Parallel()
	assertValidationField(t, `{"username":"jimmy","email":"a@b.com"}`, "password")
}

func TestCreateUser_PasswordShort(t *testing.T) {
	t.Parallel()
	assertValidationField(t, `{"username":"jimmy","email":"a@b.com","password":"short"}`, "password")
}

func assertValidationField(t *testing.T, body, field string) {
	t.Helper()
	stub := &stubRegistrar{user: registeredUser()}
	w := request{
		method:  http.MethodPost,
		path:    "/api/users",
		body:    body,
		headers: authorHeaders(),
	}.doOn(t, routerWithRegistrar(t, stub))
	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusBadRequest, &got)
	assert.Equal(t, model.ErrCodeValidationFailed, got.Code)
	require.NotEmpty(t, got.Fields)
	assert.Equal(t, field, got.Fields[0].Field)
	assert.Nil(t, stub.ctx)
}
