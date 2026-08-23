package auth

import (
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRefreshToken_Shape(t *testing.T) {
	t.Parallel()

	raw, hash, err := NewRefreshToken()
	require.NoError(t, err)

	assert.NotEmpty(t, raw)
	assert.NotEqual(t, raw, hash, "入库的必须是哈希，不能是原文")

	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	require.NoError(t, err)
	assert.Len(t, decoded, refreshTokenBytes)

	_, err = hex.DecodeString(hash)
	require.NoError(t, err, "存库形式应是十六进制")
	assert.Len(t, hash, 64, "SHA-256 十六进制固定 64 字符")
}

func TestHashRefresh_IsDeterministicAndMatchesNew(t *testing.T) {
	t.Parallel()

	raw, hash, err := NewRefreshToken()
	require.NoError(t, err)

	assert.Equal(t, hash, HashRefresh(raw))
	assert.Equal(t, HashRefresh(raw), HashRefresh(raw))
}

func TestNewRefreshToken_ValuesAreUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 32)
	for range 32 {
		raw, _, err := NewRefreshToken()
		require.NoError(t, err)

		_, dup := seen[raw]
		assert.False(t, dup, "连续生成的 token 不应重复")
		seen[raw] = struct{}{}
	}
}

// TestHashRefresh_DoesNotLookLikeBcrypt guards the choice of algorithm.
// A bcrypt hash starts with $2; if this ever does, someone "upgraded"
// the stored form to a slow hash and the refresh path will stall.
func TestHashRefresh_DoesNotLookLikeBcrypt(t *testing.T) {
	t.Parallel()

	_, hash, err := NewRefreshToken()
	require.NoError(t, err)

	assert.NotContains(t, hash, "$2")
}
