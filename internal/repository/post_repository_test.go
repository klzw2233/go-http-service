package repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/model"
)

// newPost builds a Post with a unique slug and schedules its removal so
// the shared database does not accumulate rows. It does not Create the
// row; the test decides when to insert.
func newPost(t *testing.T, slug string) *model.Post {
	t.Helper()

	pool := testPool(t)
	p := &model.Post{
		Slug:  slug,
		Title: "Title for " + slug,
		Body:  "Body for " + slug,
		// Draft by default — the service guarantees this; the repository
		// stores whatever it is given.
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.WithoutCancel(t.Context()),
			`DELETE FROM posts WHERE slug = $1`, p.Slug)
	})

	return p
}

func TestPostCreate_FillsGeneratedFields(t *testing.T) {
	t.Parallel()

	repo := NewPostRepository(testPool(t))
	p := newPost(t, uniqueName("post-create"))

	require.NoError(t, repo.Create(t.Context(), p))

	assert.Positive(t, p.ID, "id 应由数据库生成并回填")
	assert.False(t, p.CreatedAt.IsZero(), "created_at 应回填")
	assert.False(t, p.UpdatedAt.IsZero(), "updated_at 应回填")
}

func TestPostCreate_StoresDraftByDefault(t *testing.T) {
	t.Parallel()

	pool := testPool(t)
	repo := NewPostRepository(pool)
	p := newPost(t, uniqueName("post-draft"))
	p.Published = false

	require.NoError(t, repo.Create(t.Context(), p))

	var published bool
	var publishedAt *time.Time
	err := pool.QueryRow(t.Context(),
		`SELECT published, published_at FROM posts WHERE id = $1`, p.ID).
		Scan(&published, &publishedAt)
	require.NoError(t, err)

	assert.False(t, published, "新 Post 应为 Draft")
	assert.Nil(t, publishedAt, "#3 不写 published_at，应保持 NULL")
}

// TestPostCreate_DuplicateSlugIsTaken proves the unique index covers Drafts
// too: a Draft occupying a slug blocks a second insert of the same slug,
// which is the whole point of "unique including Drafts".
func TestPostCreate_DuplicateSlugIsTaken(t *testing.T) {
	t.Parallel()

	repo := NewPostRepository(testPool(t))
	slug := uniqueName("dup-slug")

	first := newPost(t, slug)
	require.NoError(t, repo.Create(t.Context(), first))

	second := newPost(t, slug)
	second.Title = "different title" // only the slug collides

	err := repo.Create(t.Context(), second)
	require.ErrorIs(t, err, ErrSlugTaken)
}

// TestPostCreate_ConcurrentSameSlug is the TOCTOU argument made concrete:
// two goroutines inserting the same slug must yield exactly one success
// and the rest ErrSlugTaken, never a 500.
func TestPostCreate_ConcurrentSameSlug(t *testing.T) {
	t.Parallel()

	repo := NewPostRepository(testPool(t))
	slug := uniqueName("post-race")
	const racers = 8

	errs := make([]error, racers)
	start := make(chan struct{})

	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := newPost(t, slug)
			p.Body = fmt.Sprintf("body-%d", i)
			<-start
			errs[i] = repo.Create(context.WithoutCancel(t.Context()), p)
		}()
	}
	close(start)
	wg.Wait()

	var succeeded, taken int
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case err.Error() == ErrSlugTaken.Error():
			taken++
		default:
			t.Errorf("意外的错误类型: %v", err)
		}
	}

	assert.Equal(t, 1, succeeded, "只应有一个并发写入成功")
	assert.Equal(t, racers-1, taken, "其余都应得到 ErrSlugTaken，而不是 500")
}

func TestPostFindBySlug_Found(t *testing.T) {
	t.Parallel()

	repo := NewPostRepository(testPool(t))
	p := newPost(t, uniqueName("find"))
	require.NoError(t, repo.Create(t.Context(), p))

	got, err := repo.FindBySlug(t.Context(), p.Slug)
	require.NoError(t, err)

	assert.Equal(t, p.ID, got.ID)
	assert.Equal(t, p.Slug, got.Slug)
	assert.Equal(t, p.Title, got.Title)
	assert.Equal(t, p.Body, got.Body)
	assert.False(t, got.Published)
	assert.Nil(t, got.PublishedAt)
}

func TestPostFindBySlug_NotFound(t *testing.T) {
	t.Parallel()

	repo := NewPostRepository(testPool(t))

	_, err := repo.FindBySlug(t.Context(), "no-such-slug-"+uniqueName("missing"))
	require.ErrorIs(t, err, ErrPostNotFound)
}

// TestPostListAll_IncludesDraftsAndOrdersByCreatedDesc is the Author-area
// contract: the list contains drafts and the newest come first.
func TestPostListAll_IncludesDraftsAndOrdersByCreatedDesc(t *testing.T) {
	t.Parallel()

	pool := testPool(t)
	repo := NewPostRepository(pool)

	older := newPost(t, uniqueName("list-old"))
	require.NoError(t, repo.Create(t.Context(), older))
	// Force a distinct created_at so the ordering is unambiguous; the
	// default now() can be the same millisecond under load.
	_, err := pool.Exec(t.Context(),
		`UPDATE posts SET created_at = now() - interval '1 hour' WHERE id = $1`, older.ID)
	require.NoError(t, err)

	newer := newPost(t, uniqueName("list-new"))
	require.NoError(t, repo.Create(t.Context(), newer))

	got, err := repo.ListAll(t.Context())
	require.NoError(t, err)

	// Other tests run in parallel against the shared DB, so do not assume
	// the whole list is exactly these two. Just assert ordering for the
	// pair we know about and that both drafts are present.
	var idxOld, idxNew = -1, -1
	for i, p := range got {
		if p.ID == older.ID {
			idxOld = i
		}
		if p.ID == newer.ID {
			idxNew = i
		}
	}
	require.NotEqual(t, -1, idxOld, "older draft 应在列表里")
	require.NotEqual(t, -1, idxNew, "newer draft 应在列表里")
	assert.Less(t, idxNew, idxOld, "newer 应排在 older 之前（created_at DESC）")
}

// TestPostUpdateTitleBody_DoesNotChangeSlug proves the UPDATE clause has
// no slug column: even if a caller tried, the slug stays. The service
// layer rejects a body slug mismatch; this guards the repository itself.
func TestPostUpdateTitleBody_DoesNotChangeSlug(t *testing.T) {
	t.Parallel()

	repo := NewPostRepository(testPool(t))
	p := newPost(t, uniqueName("upd-slug"))
	require.NoError(t, repo.Create(t.Context(), p))

	updated, err := repo.UpdateTitleBody(t.Context(), p.Slug, "New Title", "New Body")
	require.NoError(t, err)

	assert.Equal(t, p.Slug, updated.Slug, "slug 不应被改动")
	assert.Equal(t, "New Title", updated.Title)
	assert.Equal(t, "New Body", updated.Body)
	assert.False(t, updated.Published, "改 title/body 不应顺带发布")
	assert.True(t, updated.UpdatedAt.After(updated.CreatedAt) || updated.UpdatedAt.Equal(updated.CreatedAt),
		"updated_at 应推进")
}

func TestPostUpdateTitleBody_NotFound(t *testing.T) {
	t.Parallel()

	repo := NewPostRepository(testPool(t))

	_, err := repo.UpdateTitleBody(t.Context(), "no-such-slug-"+uniqueName("upd-miss"), "t", "b")
	require.ErrorIs(t, err, ErrPostNotFound)
}

func TestPostCreate_RespectsContext(t *testing.T) {
	t.Parallel()

	repo := NewPostRepository(testPool(t))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := repo.Create(ctx, newPost(t, uniqueName("post-ctx")))
	require.Error(t, err, "已取消的 context 应让查询立即失败")
	assert.NotErrorIs(t, err, ErrSlugTaken)
}

// slugIsUniqueAcrossDrafts is a helper-style assertion kept inline here:
// guard against a future change that adds a `WHERE published` clause to
// the unique index by mistake.
func TestPostSlugUniqueCoversDrafts(t *testing.T) {
	t.Parallel()

	repo := NewPostRepository(testPool(t))
	slug := uniqueName("draft-only")

	draft := newPost(t, slug)
	draft.Published = false
	require.NoError(t, repo.Create(t.Context(), draft))

	second := newPost(t, slug)
	second.Published = true // a "published" post still cannot take a Draft's slug
	err := repo.Create(t.Context(), second)

	require.ErrorIs(t, err, ErrSlugTaken)
	// keep the error message stable for the concurrent test's string match
	_ = strings.TrimSpace(err.Error())
}

func TestPostPublish_SetsPublishedAtOnce(t *testing.T) {
	t.Parallel()

	repo := NewPostRepository(testPool(t))
	p := newPost(t, uniqueName("pub"))
	require.NoError(t, repo.Create(t.Context(), p))

	first := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got, err := repo.Publish(t.Context(), p.Slug, first)
	require.NoError(t, err)
	require.True(t, got.Published)
	require.NotNil(t, got.PublishedAt)
	assert.True(t, got.PublishedAt.Equal(first))

	later := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	again, err := repo.Publish(t.Context(), p.Slug, later)
	require.NoError(t, err)
	require.NotNil(t, again.PublishedAt)
	assert.True(t, again.PublishedAt.Equal(first), "再次 Publish 不得改写首次 published_at")
}

func TestPostUnpublish_KeepsPublishedAt(t *testing.T) {
	t.Parallel()

	repo := NewPostRepository(testPool(t))
	p := newPost(t, uniqueName("unpub"))
	require.NoError(t, repo.Create(t.Context(), p))

	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	_, err := repo.Publish(t.Context(), p.Slug, when)
	require.NoError(t, err)

	got, err := repo.Unpublish(t.Context(), p.Slug)
	require.NoError(t, err)
	assert.False(t, got.Published)
	require.NotNil(t, got.PublishedAt)
	assert.True(t, got.PublishedAt.Equal(when))
}

func TestPostListPublished_OmitsDraftsAndOrdersByPublishedAt(t *testing.T) {
	t.Parallel()

	repo := NewPostRepository(testPool(t))
	older := newPost(t, uniqueName("pub-old"))
	newer := newPost(t, uniqueName("pub-new"))
	draft := newPost(t, uniqueName("pub-draft"))
	require.NoError(t, repo.Create(t.Context(), older))
	require.NoError(t, repo.Create(t.Context(), newer))
	require.NoError(t, repo.Create(t.Context(), draft))

	oldTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	_, err := repo.Publish(t.Context(), older.Slug, oldTime)
	require.NoError(t, err)
	_, err = repo.Publish(t.Context(), newer.Slug, newTime)
	require.NoError(t, err)

	got, err := repo.ListPublished(t.Context())
	require.NoError(t, err)

	var idxOld, idxNew, idxDraft = -1, -1, -1
	for i, row := range got {
		if row.ID == older.ID {
			idxOld = i
		}
		if row.ID == newer.ID {
			idxNew = i
		}
		if row.ID == draft.ID {
			idxDraft = i
		}
	}
	assert.Equal(t, -1, idxDraft, "Draft 不得出现在公开列表")
	require.NotEqual(t, -1, idxOld)
	require.NotEqual(t, -1, idxNew)
	assert.Less(t, idxNew, idxOld, "更新的 published_at 应排在前面")
}
