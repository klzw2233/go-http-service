package model

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hashLikeValue stands in for a real bcrypt digest. Tests assert it never
// appears in anything sent to a client.
const hashLikeValue = "$2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123"

func testUser() User {
	return User{
		ID:           7,
		Username:     "jimmy",
		Email:        "jimmy@example.com",
		PasswordHash: hashLikeValue,
		CreatedAt:    time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC),
	}
}

// TestUserNeverSerialisesPasswordHash guards the struct tag. Losing it in
// a refactor would put password hashes on the wire, and nothing else
// about the code would look wrong.
func TestUserNeverSerialisesPasswordHash(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(testUser())
	require.NoError(t, err)

	assert.NotContains(t, string(raw), hashLikeValue, "序列化 User 泄露了密码哈希")
	assert.NotContains(t, string(raw), "password_hash")
	assert.NotContains(t, string(raw), "PasswordHash")
}

// TestUserResponseOmitsPasswordHash covers the second line of defence.
// The projection cannot leak a hash even if the struct tag on User is
// removed, because the field does not exist on the response type.
func TestUserResponseOmitsPasswordHash(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(NewUserResponse(testUser()))
	require.NoError(t, err)

	assert.NotContains(t, string(raw), hashLikeValue)
	assert.NotContains(t, string(raw), "password")

	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &decoded))

	keys := make([]string, 0, len(decoded))
	for k := range decoded {
		keys = append(keys, k)
	}
	assert.ElementsMatch(t, []string{"id", "username", "email", "created_at"}, keys,
		"响应字段集合变了，新增字段前请确认它可以公开")
}

func TestNewUserResponse(t *testing.T) {
	t.Parallel()

	u := testUser()
	got := NewUserResponse(u)

	assert.Equal(t, u.ID, got.ID)
	assert.Equal(t, u.Username, got.Username)
	assert.Equal(t, u.Email, got.Email)
	assert.True(t, got.CreatedAt.Equal(u.CreatedAt))
}

// TestCreateUserRequestBindingTags pins the validation rules, since they
// are expressed as string tags that no compiler checks.
func TestCreateUserRequestBindingTags(t *testing.T) {
	t.Parallel()

	tag := bindingTag(t, CreateUserRequest{}, "Username")
	assert.Contains(t, tag, "required")
	assert.Contains(t, tag, "alphanum")
	assert.Contains(t, tag, "min=3")
	assert.Contains(t, tag, "max=32")

	tag = bindingTag(t, CreateUserRequest{}, "Email")
	assert.Contains(t, tag, "email")

	// The password max is deliberately absent: bcrypt's limit is 72
	// BYTES and a binding tag counts runes, so the check lives in the
	// service layer. See MaxPasswordBytes.
	tag = bindingTag(t, CreateUserRequest{}, "Password")
	assert.Contains(t, tag, "min=8")
	assert.NotContains(t, tag, "max=",
		"密码上限不能用 binding tag 表达，它按字符计而 bcrypt 按字节截断")
}

// TestMaxPasswordBytesMatchesBcrypt records why the constant is 72.
func TestMaxPasswordBytesMatchesBcrypt(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 72, MaxPasswordBytes, "bcrypt 只使用前 72 字节")

	// A 72-character CJK password is well over the byte limit, which is
	// the whole reason a rune-based tag cannot express this rule.
	cjk := ""
	for range MaxPasswordBytes {
		cjk += "密"
	}
	assert.Len(t, []rune(cjk), MaxPasswordBytes)
	assert.Greater(t, len(cjk), MaxPasswordBytes,
		"72 个汉字应远超 72 字节，否则这条测试没有意义")
}

// bindingTag returns the binding tag of a named field.
func bindingTag(t *testing.T, v any, field string) string {
	t.Helper()

	f, ok := reflect.TypeOf(v).FieldByName(field)
	require.True(t, ok, "字段 %q 不存在", field)

	return f.Tag.Get("binding")
}
