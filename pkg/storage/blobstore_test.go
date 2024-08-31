package storage

import (
	"testing"

	"github.com/EncrypteDL/CryptFS/pkg/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlobStore(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	store := NewBlobStore(NewInMemoryStore())
	t.Run("same value, same key", func(t *testing.T) {
		value := message.RandomBytes()
		key1, err1 := store.Put(value)
		require.NoError(err1)

		key2, err2 := store.Put(value)
		require.NoError(err2)

		assert.Len(key1, 64)
		assert.Len(key2, 64)
		assert.Equal(key1, key2)
	})
	t.Run("different values, different keys", func(t *testing.T) {
		value1 := message.RandomBytes()
		value2 := message.RandomBytes()
		key1, err1 := store.Put(value1)
		require.NoError(err1)

		key2, err2 := store.Put(value2)
		require.NoError(err2)

		assert.Len(key1, 64)
		assert.Len(key2, 64)
		assert.NotEqual(key1, key2)
	})
	t.Run("what you put is what you get", func(t *testing.T) {
		before := message.RandomBytes()
		key, err := store.Put(before)
		assert.NoError(err)
		after, err := store.Get(key)
		assert.NoError(err)
		assert.Equal(before, after)
	})
}
