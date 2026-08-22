package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/model"
)

// nameCounter keeps generated usernames unique so tests can run in
// parallel against one shared database without truncating tables
// between them.
var nameCounter atomic.Int64

// uniqueName returns a username unused by any other test.
func uniqueName(prefix string) string {
	return fmt.Sprintf("%s%d", prefix, nameCounter.Add(1))
}

// testPool connects to the test database, or skips.
//
// Database-backed tests are opt-in via TEST_DATABASE_URL so that
// `go test ./...` stays green without PostgreSQL installed. CI sets it.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL 未设置，跳过需要真实数据库的测试")
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second

	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, pool.Ping(t.Context()))
	t.Cleanup(pool.Close)

	return pool
}

// newUser builds a user with unique credentials, and schedules its
// removal so the shared database does not accumulate rows.
func newUser(t *testing.T, pool *pgxpool.Pool, name string) *model.User {
	t.Helper()

	u := &model.User{
		Username:     name,
		Email:        name + "@example.com",
		PasswordHash: "$2a$04$notarealhashbutthatisfineforstoragetests00000000000000",
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.WithoutCancel(t.Context()),
			`DELETE FROM users WHERE lower(username) = lower($1)`, u.Username)
	})

	return u
}

func TestCreate_FillsGeneratedFields(t *testing.T) {
	t.Parallel()

	pool := testPool(t)
	repo := NewUserRepository(pool)

	u := newUser(t, pool, uniqueName("create"))
	before := time.Now()

	require.NoError(t, repo.Create(t.Context(), u))

	assert.Positive(t, u.ID, "id 应由数据库生成并回填")
	assert.False(t, u.CreatedAt.IsZero(), "created_at 应回填")
	assert.False(t, u.UpdatedAt.IsZero(), "updated_at 应回填")
	assert.WithinDuration(t, before, u.CreatedAt, time.Minute)
}

func TestCreate_PersistsValues(t *testing.T) {
	t.Parallel()

	pool := testPool(t)
	repo := NewUserRepository(pool)

	u := newUser(t, pool, uniqueName("persist"))
	require.NoError(t, repo.Create(t.Context(), u))

	var username, email, hash string
	err := pool.QueryRow(t.Context(),
		`SELECT username, email, password_hash FROM users WHERE id = $1`, u.ID).
		Scan(&username, &email, &hash)
	require.NoError(t, err)

	// Case is preserved as entered, even though uniqueness ignores it.
	assert.Equal(t, u.Username, username)
	assert.Equal(t, u.Email, email)
	assert.Equal(t, u.PasswordHash, hash)
}

func TestCreate_DuplicateUsername(t *testing.T) {
	t.Parallel()

	pool := testPool(t)
	repo := NewUserRepository(pool)

	name := uniqueName("dupuser")
	first := newUser(t, pool, name)
	require.NoError(t, repo.Create(t.Context(), first))

	second := newUser(t, pool, name)
	second.Email = uniqueName("other") + "@example.com" // 只让用户名冲突

	err := repo.Create(t.Context(), second)

	require.ErrorIs(t, err, ErrUsernameTaken)
}

func TestCreate_DuplicateEmail(t *testing.T) {
	t.Parallel()

	pool := testPool(t)
	repo := NewUserRepository(pool)

	first := newUser(t, pool, uniqueName("dupmail"))
	require.NoError(t, repo.Create(t.Context(), first))

	second := newUser(t, pool, uniqueName("dupmail"))
	second.Email = first.Email // 只让邮箱冲突

	err := repo.Create(t.Context(), second)

	require.ErrorIs(t, err, ErrEmailTaken)
}

// TestCreate_DuplicateIsCaseInsensitive proves the functional unique
// index on lower(username) is doing its job: Jimmy and jimmy are the
// same account, so the second registration must be refused.
func TestCreate_DuplicateIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	pool := testPool(t)
	repo := NewUserRepository(pool)

	name := uniqueName("CaseTest")
	first := newUser(t, pool, name)
	require.NoError(t, repo.Create(t.Context(), first))

	second := newUser(t, pool, strings.ToUpper(name))
	second.Email = uniqueName("case") + "@example.com"

	err := repo.Create(t.Context(), second)

	require.ErrorIs(t, err, ErrUsernameTaken,
		"大小写不同的同名应视为重复，否则同一个人能注册两次")
}

func TestCreate_DuplicateEmailIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	pool := testPool(t)
	repo := NewUserRepository(pool)

	first := newUser(t, pool, uniqueName("mailcase"))
	require.NoError(t, repo.Create(t.Context(), first))

	second := newUser(t, pool, uniqueName("mailcase"))
	second.Email = strings.ToUpper(first.Email)

	err := repo.Create(t.Context(), second)

	require.ErrorIs(t, err, ErrEmailTaken)
}

// TestCreate_ConcurrentSameUsername is why Create does not pre-check
// availability. A check-then-insert would let both goroutines past the
// check; relying on the unique index means exactly one wins and the
// other gets a clean domain error rather than a 500.
func TestCreate_ConcurrentSameUsername(t *testing.T) {
	t.Parallel()

	pool := testPool(t)
	repo := NewUserRepository(pool)

	name := uniqueName("race")
	const racers = 8

	errs := make([]error, racers)
	start := make(chan struct{})

	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			u := &model.User{
				Username:     name,
				Email:        fmt.Sprintf("%s-%d@example.com", name, i),
				PasswordHash: "$2a$04$notarealhash000000000000000000000000000000000000000000",
			}
			<-start
			errs[i] = repo.Create(context.WithoutCancel(t.Context()), u)
		}()
	}
	close(start)
	wg.Wait()

	t.Cleanup(func() {
		_, _ = pool.Exec(context.WithoutCancel(t.Context()),
			`DELETE FROM users WHERE lower(username) = lower($1)`, name)
	})

	var succeeded, taken int
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case err.Error() == ErrUsernameTaken.Error():
			taken++
		default:
			t.Errorf("意外的错误类型: %v", err)
		}
	}

	assert.Equal(t, 1, succeeded, "只应有一个并发写入成功")
	assert.Equal(t, racers-1, taken, "其余都应得到 ErrUsernameTaken，而不是 500 级错误")
}

func TestCreate_RespectsContext(t *testing.T) {
	t.Parallel()

	pool := testPool(t)
	repo := NewUserRepository(pool)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := repo.Create(ctx, newUser(t, pool, uniqueName("ctx")))

	require.Error(t, err, "已取消的 context 应让查询立即失败")
	assert.NotErrorIs(t, err, ErrUsernameTaken)
}
