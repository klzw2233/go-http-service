package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"go-http-service/internal/model"
	"go-http-service/internal/repository"
)

// stubStore records what it was asked to create and returns a canned
// error. The service has no other dependency, so it needs no database.
type stubStore struct {
	created *model.User
	ctx     context.Context
	err     error
	calls   int
}

func (s *stubStore) Create(ctx context.Context, u *model.User) error {
	s.calls++
	s.ctx = ctx
	if s.err != nil {
		return s.err
	}
	// Mimic the database filling in generated columns.
	u.ID = 1
	s.created = u
	return nil
}

// newTestService uses the cheapest bcrypt cost. The default takes about
// 60ms per hash by design; across this file that would be seconds, and
// worse under -race.
func newTestService(store *stubStore) *UserService {
	return NewUserService(store, WithBcryptCost(bcrypt.MinCost))
}

func validInput() RegisterInput {
	return RegisterInput{
		Username: "jimmy",
		Email:    "jimmy@example.com",
		Password: "correct-horse-battery",
	}
}

func TestRegister_StoresHashNotPlaintext(t *testing.T) {
	t.Parallel()

	store := &stubStore{}
	in := validInput()

	user, err := newTestService(store).Register(t.Context(), in)
	require.NoError(t, err)

	assert.Equal(t, in.Username, user.Username)
	assert.Equal(t, in.Email, user.Email)

	assert.NotEqual(t, in.Password, user.PasswordHash, "密码不能明文入库")
	assert.NotContains(t, user.PasswordHash, in.Password)
	assert.True(t, strings.HasPrefix(user.PasswordHash, "$2"),
		"应是 bcrypt 格式，实际: %s", user.PasswordHash)

	// The stored hash must actually verify, or login could never work.
	assert.NoError(t,
		bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.Password)))
}

// TestRegister_RejectsPasswordOverByteLimit is the reason the length
// check lives here rather than in a binding tag.
//
// bcrypt uses only the first 72 bytes and silently drops the rest, so
// without this check every password sharing its first 72 bytes would
// open the same account. gin's validator measures strings in runes, so
// `max=72` would happily accept the 216-byte value below.
func TestRegister_RejectsPasswordOverByteLimit(t *testing.T) {
	t.Parallel()

	cjk := strings.Repeat("密", model.MaxPasswordBytes) // 72 字符 = 216 字节
	require.Len(t, []rune(cjk), model.MaxPasswordBytes)
	require.Greater(t, len(cjk), model.MaxPasswordBytes)

	store := &stubStore{}
	in := validInput()
	in.Password = cjk

	_, err := newTestService(store).Register(t.Context(), in)

	require.ErrorIs(t, err, ErrPasswordTooLong)
	assert.Zero(t, store.calls, "校验失败时不应触碰数据库")
}

func TestRegister_AcceptsPasswordAtByteLimit(t *testing.T) {
	t.Parallel()

	store := &stubStore{}
	in := validInput()
	in.Password = strings.Repeat("a", model.MaxPasswordBytes)

	_, err := newTestService(store).Register(t.Context(), in)

	assert.NoError(t, err, "恰好 72 字节应当接受")
}

// TestRegister_TranslatesRepositoryErrors covers the layering rule: the
// handler matches on service errors, so it never imports repository.
func TestRegister_TranslatesRepositoryErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		repoErr error
		want    error
	}{
		{"用户名冲突", repository.ErrUsernameTaken, ErrUsernameTaken},
		{"邮箱冲突", repository.ErrEmailTaken, ErrEmailTaken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &stubStore{err: tt.repoErr}

			_, err := newTestService(store).Register(t.Context(), validInput())

			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestRegister_WrapsUnknownRepositoryError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("connection reset")
	store := &stubStore{err: sentinel}

	_, err := newTestService(store).Register(t.Context(), validInput())

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel, "未知错误应保留原因以便排查")
	assert.NotErrorIs(t, err, ErrUsernameTaken)
	assert.NotErrorIs(t, err, ErrEmailTaken)
}

// TestRegister_PassesContextThrough matters because the request deadline
// installed by REQUEST_TIMEOUT only reaches the query if every layer
// forwards its context. See CLAUDE.md 4.6.
func TestRegister_PassesContextThrough(t *testing.T) {
	t.Parallel()

	type key struct{}
	ctx := context.WithValue(t.Context(), key{}, "marker")

	store := &stubStore{}

	_, err := newTestService(store).Register(ctx, validInput())
	require.NoError(t, err)

	require.NotNil(t, store.ctx)
	assert.Equal(t, "marker", store.ctx.Value(key{}),
		"repository 收到的应是调用方的 context，而不是新建的")
}

func TestNewUserService_DefaultsToStandardCost(t *testing.T) {
	t.Parallel()

	assert.Equal(t, bcrypt.DefaultCost, NewUserService(&stubStore{}).cost,
		"生产默认必须是 DefaultCost，MinCost 只该出现在测试里")
}
