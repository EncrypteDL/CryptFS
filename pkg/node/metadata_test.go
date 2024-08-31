package node

import (
	"math/rand"
	"testing"
	"time"

	"github.com/EncrypteDL/CryptFS/pkg/message"
	"github.com/EncrypteDL/CryptFS/pkg/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeSerialization(t *testing.T) {
	g := NewInodeNumbersGenerator()
	go g.Start()
	defer g.Stop()
	rand.Seed(time.Now().UnixNano())
	store := storage.NewInMemoryStore()
	versioned := storage.NewVersionedWrapper(store)
	factory := &CryptNodeFactory{InodeGenerator: g, Metadata: versioned}
	for i := 0; i < 100; i++ {
		before := randomNode(t, factory)
		err := before.saveMetadata()
		require.Nil(t, err)
		after, err := factory.allocateNode()
		require.Nil(t, err)
		err = after.LoadMetadata(before.Key)
		require.Nil(t, err)
		assert.Equal(t, before.User, after.User)
		assert.Equal(t, before.Group, after.Group)
		assert.Equal(t, before.Mode, after.Mode)
		assert.Equal(t, before.Time.UnixNano(), after.Time.UnixNano())
		assert.Equal(t, before.version, after.version)
		assert.EqualValues(t, before.Key, after.Key)
		assert.EqualValues(t, before.contentKey, after.contentKey)
	}
}

func randomNode(t *testing.T, factory *CryptNodeFactory) *CryptNode {
	node, err := factory.allocateNode()
	require.Nil(t, err)
	node.User = rand.Uint32()
	node.Group = rand.Uint32()
	node.Mode = rand.Uint32()
	node.Time = time.Unix(rand.Int63(), rand.Int63())
	keyLen := rand.Intn(10)
	node.contentKey = make([]byte, keyLen)
	rand.Read(node.contentKey)
	node.version = rand.Uint64()
	node.xattrs = make(map[string][]byte)
	nxattrs := rand.Intn(4)
	for ; nxattrs > 0; nxattrs-- {
		node.xattrs[message.RandomString()] = message.RandomBytes()
	}
	return node
}
