package service

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"go-http-service/internal/auth"
	"go-http-service/internal/model"
	"go-http-service/internal/repository"
)

const (
	testUsername = "jimmy"
	testPassword = "correct-horse-battery"
)

// stubUserFinder serves canned lookups so these tests need no database.
type stubUserFinder struct {
	user *model.User
	err  error
	ctx  context.Context
}

func (s *stubUserFinder) FindByUsername(ctx context.Context, _ string) (*model.User, error) {
	s.ctx = ctx
	if s.err != nil {
		return nil, s.err
	}
	return s.user, nil
}

func (s *stubUserFinder) FindByID(ctx context.Context, _ int64) (*model.User, error) {
	s.ctx = ctx
	if s.err != nil {
		return nil, s.err
	}
	return s.user, nil
}

// stubIssuer records what it was asked to sign.
type stubIssuer struct {
	userID int64
	token  string
	err    error
}

func (s *stubIssuer) IssueAccess(userID int64) (string, time.Time, error) {
	s.userID = userID
	if s.err != nil {
		return "", time.Time{}, s.err
	}
	return s.token, time.Date(2026, 8, 23, 12, 15, 0, 0, time.UTC), nil
}

const testRefreshTTL = 30 * 24 * time.Hour

type storedRefresh struct {
	userID    int64
	hash      string
	expiresAt time.Time
	revoked   bool
}

// stubRefreshStore records hashes so tests can prove the raw token never
// lands in storage, and can drive rotate/replay without a database.
type stubRefreshStore struct {
	tokens map[string]*storedRefresh
	err    error
	ctx    context.Context
}

func newStubRefreshStore() *stubRefreshStore {
	return &stubRefreshStore{tokens: make(map[string]*storedRefresh)}
}

func (s *stubRefreshStore) Store(ctx context.Context, userID int64, hash string, expiresAt time.Time) error {
	s.ctx = ctx
	if s.err != nil {
		return s.err
	}
	s.tokens[hash] = &storedRefresh{userID: userID, hash: hash, expiresAt: expiresAt}
	return nil
}

func (s *stubRefreshStore) TryRotate(ctx context.Context, oldHash, newHash string, newExpiry, now time.Time) (int64, error) {
	s.ctx = ctx
	if s.err != nil {
		return 0, s.err
	}

	tok, ok := s.tokens[oldHash]
	if !ok {
		return 0, repository.ErrRefreshTokenNotFound
	}
	if tok.revoked {
		for _, t := range s.tokens {
			if t.userID == tok.userID {
				t.revoked = true
			}
		}
		return 0, repository.ErrRefreshTokenReused
	}
	if !tok.expiresAt.After(now) {
		return 0, repository.ErrRefreshTokenNotFound
	}

	tok.revoked = true
	s.tokens[newHash] = &storedRefresh{userID: tok.userID, hash: newHash, expiresAt: newExpiry}
	return tok.userID, nil
}

func (s *stubRefreshStore) RevokeByHash(ctx context.Context, hash string, now time.Time) error {
	s.ctx = ctx
	if s.err != nil {
		return s.err
	}

	tok, ok := s.tokens[hash]
	if !ok || tok.revoked || !tok.expiresAt.After(now) {
		return repository.ErrRefreshTokenNotFound
	}
	tok.revoked = true
	return nil
}

func (s *stubRefreshStore) activeCount(userID int64) int {
	n := 0
	for _, tok := range s.tokens {
		if tok.userID == userID && !tok.revoked {
			n++
		}
	}
	return n
}

// storedUser builds a user whose hash really matches testPassword, so
// the comparison under test is a real bcrypt verification.
func storedUser(t *testing.T) *model.User {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	require.NoError(t, err)

	return &model.User{
		ID:           7,
		Username:     testUsername,
		Email:        "jimmy@example.com",
		PasswordHash: string(hash),
	}
}

func newTestAuthService(t *testing.T, users userFinder, tokens tokenIssuer) *AuthService {
	t.Helper()
	return newTestAuthServiceWith(t, users, tokens, newStubRefreshStore())
}

func newTestAuthServiceWith(t *testing.T, users userFinder, tokens tokenIssuer, refresh refreshStore, extra ...AuthOption) *AuthService {
	t.Helper()

	// MinCost for the same reason the user service uses it: the default
	// spends ~60ms per hash, which is correct in production and
	// intolerable across a test file under -race.
	opts := append([]AuthOption{WithAuthBcryptCost(bcrypt.MinCost)}, extra...)
	svc, err := NewAuthService(users, tokens, refresh, testRefreshTTL, opts...)
	require.NoError(t, err)

	return svc
}

func TestLogin_Succeeds(t *testing.T) {
	t.Parallel()

	user := storedUser(t)
	issuer := &stubIssuer{token: "signed.jwt.value"}

	result, err := newTestAuthService(t, &stubUserFinder{user: user}, issuer).
		Login(t.Context(), testUsername, testPassword)

	require.NoError(t, err)
	assert.Equal(t, user.ID, result.User.ID)
	assert.Equal(t, "signed.jwt.value", result.AccessToken)
	assert.NotEmpty(t, result.RefreshToken)
	assert.False(t, result.ExpiresAt.IsZero())

	assert.Equal(t, user.ID, issuer.userID, "签发的 token 应属于查到的那个用户")
}

func TestLogin_StoresHashNotRawToken(t *testing.T) {
	t.Parallel()

	store := newStubRefreshStore()
	user := storedUser(t)

	result, err := newTestAuthServiceWith(t, &stubUserFinder{user: user},
		&stubIssuer{token: "t"}, store).
		Login(t.Context(), testUsername, testPassword)
	require.NoError(t, err)

	require.Len(t, store.tokens, 1)
	for hash, tok := range store.tokens {
		assert.Equal(t, auth.HashRefresh(result.RefreshToken), hash)
		assert.NotEqual(t, result.RefreshToken, hash)
		assert.Equal(t, user.ID, tok.userID)
		assert.False(t, tok.revoked)
	}
}

// TestLogin_SameErrorForMissingUserAndWrongPassword is the rule that
// keeps the endpoint from being a username oracle.
func TestLogin_SameErrorForMissingUserAndWrongPassword(t *testing.T) {
	t.Parallel()

	_, missingErr := newTestAuthService(t,
		&stubUserFinder{err: repository.ErrUserNotFound}, &stubIssuer{}).
		Login(t.Context(), "nobody", testPassword)

	_, wrongErr := newTestAuthService(t,
		&stubUserFinder{user: storedUser(t)}, &stubIssuer{}).
		Login(t.Context(), testUsername, "wrong-password")

	require.ErrorIs(t, missingErr, ErrInvalidCredentials)
	require.ErrorIs(t, wrongErr, ErrInvalidCredentials)

	assert.Equal(t, missingErr.Error(), wrongErr.Error(),
		"用户不存在与密码错误必须返回完全相同的错误，否则接口就是用户名枚举器")
}

// TestLogin_TimingIsComparable covers the half of that defence an
// identical error cannot provide.
//
// Returning early for an unknown user skips bcrypt entirely, and that
// gap is large enough to measure: an attacker who times the response
// learns which usernames exist even though both answers read the same.
// The dummy comparison closes it.
//
// Asserted on medians over many runs, with a deliberately loose bound.
// A tight one would flake on a busy machine, and the failure this
// guards against is a whole missing bcrypt call, not a few percent.
// Removing the dummy comparison makes it fail with 34ns against 57ms.
//
// Behind -short because it needs a production bcrypt cost to be
// meaningful, which costs ~14s under -race. CI runs the full suite, so
// it is still checked on every push.
func TestLogin_TimingIsComparable(t *testing.T) {
	if testing.Short() {
		t.Skip("计时测试需要真实 bcrypt 成本，耗时较长，-short 时跳过")
	}
	t.Parallel()

	// A real cost, not MinCost: at MinCost both paths are fast enough
	// that a missing comparison would not show up.
	const cost = 10

	user := storedUser(t)
	realHash, err := bcrypt.GenerateFromPassword([]byte(testPassword), cost)
	require.NoError(t, err)
	user.PasswordHash = string(realHash)

	found, err := NewAuthService(&stubUserFinder{user: user}, &stubIssuer{},
		newStubRefreshStore(), testRefreshTTL, WithAuthBcryptCost(cost))
	require.NoError(t, err)

	missing, err := NewAuthService(&stubUserFinder{err: repository.ErrUserNotFound},
		&stubIssuer{}, newStubRefreshStore(), testRefreshTTL, WithAuthBcryptCost(cost))
	require.NoError(t, err)

	const samples = 7

	median := func(svc *AuthService, username string) time.Duration {
		times := make([]time.Duration, samples)
		for i := range samples {
			start := time.Now()
			_, _ = svc.Login(t.Context(), username, "some-wrong-password")
			times[i] = time.Since(start)
		}
		sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
		return times[samples/2]
	}

	wrongPassword := median(found, testUsername)
	noSuchUser := median(missing, "nobody")

	require.Positive(t, wrongPassword)

	ratio := float64(noSuchUser) / float64(wrongPassword)
	assert.Greater(t, ratio, 0.5,
		"用户不存在的路径快了太多（%s vs %s），假哈希比对可能没生效",
		noSuchUser, wrongPassword)
	assert.Less(t, ratio, 2.0,
		"两条路径耗时差距过大（%s vs %s）", noSuchUser, wrongPassword)
}

func TestLogin_WrapsUnexpectedLookupError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("connection reset")

	_, err := newTestAuthService(t, &stubUserFinder{err: sentinel}, &stubIssuer{}).
		Login(t.Context(), testUsername, testPassword)

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel, "非预期错误应保留原因以便排查")
	assert.NotErrorIs(t, err, ErrInvalidCredentials,
		"数据库故障不该被当成凭据错误，否则真实故障会被 401 掩盖")
}

func TestLogin_WrapsIssuerFailure(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("signing key unavailable")

	_, err := newTestAuthService(t,
		&stubUserFinder{user: storedUser(t)}, &stubIssuer{err: sentinel}).
		Login(t.Context(), testUsername, testPassword)

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	assert.NotErrorIs(t, err, ErrInvalidCredentials)
}

func TestLogin_PassesContextThrough(t *testing.T) {
	t.Parallel()

	type key struct{}
	ctx := context.WithValue(t.Context(), key{}, "marker")

	finder := &stubUserFinder{user: storedUser(t)}

	_, err := newTestAuthService(t, finder, &stubIssuer{token: "t"}).
		Login(ctx, testUsername, testPassword)
	require.NoError(t, err)

	require.NotNil(t, finder.ctx)
	assert.Equal(t, "marker", finder.ctx.Value(key{}),
		"repository 收到的应是调用方的 context")
}

func TestUserByID_Succeeds(t *testing.T) {
	t.Parallel()

	user := storedUser(t)

	got, err := newTestAuthService(t, &stubUserFinder{user: user}, &stubIssuer{}).
		UserByID(t.Context(), user.ID)

	require.NoError(t, err)
	assert.Equal(t, user.ID, got.ID)
}

// TestUserByID_DeletedAccountIsUnauthenticated covers a token that
// verified but whose owner is gone. The credential no longer identifies
// anyone, which is an authentication failure rather than a 500.
func TestUserByID_DeletedAccountIsUnauthenticated(t *testing.T) {
	t.Parallel()

	_, err := newTestAuthService(t,
		&stubUserFinder{err: repository.ErrUserNotFound}, &stubIssuer{}).
		UserByID(t.Context(), 999)

	require.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestNewAuthService_DefaultsToStandardCost(t *testing.T) {
	t.Parallel()

	svc, err := NewAuthService(&stubUserFinder{}, &stubIssuer{}, newStubRefreshStore(), testRefreshTTL)
	require.NoError(t, err)

	cost, err := bcrypt.Cost(svc.dummyHash)
	require.NoError(t, err)

	assert.Equal(t, bcrypt.DefaultCost, cost,
		"假哈希的成本必须与生产成本一致，否则两条路径的耗时又对不上了")
}

func TestNewAuthService_RejectsNonPositiveTTL(t *testing.T) {
	t.Parallel()

	_, err := NewAuthService(&stubUserFinder{}, &stubIssuer{}, newStubRefreshStore(), 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be positive")
}

func TestRefresh_Succeeds(t *testing.T) {
	t.Parallel()

	store := newStubRefreshStore()
	user := storedUser(t)
	issuer := &stubIssuer{token: "next.jwt"}

	svc := newTestAuthServiceWith(t, &stubUserFinder{user: user}, issuer, store)

	login, err := svc.Login(t.Context(), testUsername, testPassword)
	require.NoError(t, err)

	refreshed, err := svc.Refresh(t.Context(), login.RefreshToken)
	require.NoError(t, err)

	assert.Equal(t, "next.jwt", refreshed.AccessToken)
	assert.NotEmpty(t, refreshed.RefreshToken)
	assert.NotEqual(t, login.RefreshToken, refreshed.RefreshToken, "轮转必须换发新的 refresh token")
	assert.Equal(t, user.ID, issuer.userID)
	assert.Equal(t, 1, store.activeCount(user.ID))
}

func TestRefresh_ReplayRevokesFamily(t *testing.T) {
	t.Parallel()

	store := newStubRefreshStore()
	user := storedUser(t)
	svc := newTestAuthServiceWith(t, &stubUserFinder{user: user},
		&stubIssuer{token: "t"}, store)

	login, err := svc.Login(t.Context(), testUsername, testPassword)
	require.NoError(t, err)

	_, err = svc.Refresh(t.Context(), login.RefreshToken)
	require.NoError(t, err)

	_, err = svc.Refresh(t.Context(), login.RefreshToken)
	require.ErrorIs(t, err, ErrRefreshTokenReused)
	assert.Zero(t, store.activeCount(user.ID),
		"重放已用过的 refresh token 应撤销该用户的全部会话")
}

func TestRefresh_UnknownToken(t *testing.T) {
	t.Parallel()

	_, err := newTestAuthService(t, &stubUserFinder{}, &stubIssuer{}).
		Refresh(t.Context(), "not-a-real-token")

	require.ErrorIs(t, err, ErrInvalidCredentials)
	assert.NotErrorIs(t, err, ErrRefreshTokenReused,
		"从未见过的 token 不是重放，不应触发全家撤销的语义")
}

func TestRefresh_ExpiredToken(t *testing.T) {
	t.Parallel()

	store := newStubRefreshStore()
	user := storedUser(t)

	start := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	clock := start
	svc := newTestAuthServiceWith(t, &stubUserFinder{user: user},
		&stubIssuer{token: "t"}, store, WithAuthClock(func() time.Time { return clock }))

	login, err := svc.Login(t.Context(), testUsername, testPassword)
	require.NoError(t, err)

	clock = start.Add(testRefreshTTL + time.Second)

	_, err = svc.Refresh(t.Context(), login.RefreshToken)
	require.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestLogout_RevokesToken(t *testing.T) {
	t.Parallel()

	store := newStubRefreshStore()
	user := storedUser(t)
	svc := newTestAuthServiceWith(t, &stubUserFinder{user: user},
		&stubIssuer{token: "t"}, store)

	login, err := svc.Login(t.Context(), testUsername, testPassword)
	require.NoError(t, err)

	require.NoError(t, svc.Logout(t.Context(), login.RefreshToken))
	assert.Zero(t, store.activeCount(user.ID))

	err = svc.Logout(t.Context(), login.RefreshToken)
	require.ErrorIs(t, err, ErrInvalidCredentials,
		"再次登出必须与无效 token 无法区分")
}

func TestLogout_DoesNotTreatReplayAsTheft(t *testing.T) {
	t.Parallel()

	store := newStubRefreshStore()
	user := storedUser(t)
	svc := newTestAuthServiceWith(t, &stubUserFinder{user: user},
		&stubIssuer{token: "t"}, store)

	first, err := svc.Login(t.Context(), testUsername, testPassword)
	require.NoError(t, err)
	second, err := svc.Login(t.Context(), testUsername, testPassword)
	require.NoError(t, err)

	require.NoError(t, svc.Logout(t.Context(), first.RefreshToken))
	err = svc.Logout(t.Context(), first.RefreshToken)
	require.ErrorIs(t, err, ErrInvalidCredentials)

	assert.Equal(t, 1, store.activeCount(user.ID),
		"对已撤销 token 再 logout 不应误杀其他会话")
	assert.NoError(t, svc.Logout(t.Context(), second.RefreshToken))
}

func TestRefresh_PassesContextThrough(t *testing.T) {
	t.Parallel()

	type key struct{}
	ctx := context.WithValue(t.Context(), key{}, "marker")

	store := newStubRefreshStore()
	user := storedUser(t)
	svc := newTestAuthServiceWith(t, &stubUserFinder{user: user},
		&stubIssuer{token: "t"}, store)

	login, err := svc.Login(t.Context(), testUsername, testPassword)
	require.NoError(t, err)

	_, err = svc.Refresh(ctx, login.RefreshToken)
	require.NoError(t, err)
	assert.Equal(t, "marker", store.ctx.Value(key{}))
}
