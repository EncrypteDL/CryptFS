package node

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/EncrypteDL/CryptFS/pkg/message"
	"github.com/EncrypteDL/CryptFS/pkg/storage"
	sync "github.com/sasha-s/go-deadlock"
	log "github.com/sirupsen/logrus"
)

// CryptNodeFactory creates filesystem nodes
type CryptNodeFactory struct {
	Root           *CryptNode
	InodeGenerator *InodeNumbersGenerator
	Metadata       storage.VersionedStore
	Blobs          *storage.BlobStoreWrapper
	mu             sync.Mutex
	known          map[[NodeKeyLen]byte]*CryptNode
}

func (factory *CryptNodeFactory) allocateNode() (*CryptNode, error) {
	var node CryptNode
	node.factory = factory
	node.Time = time.Now()
	n, err := rand.Read(node.Key[:])
	if err != nil {
		return nil, err
	}
	if n != NodeKeyLen {
		return nil, fmt.Errorf("could only read %d of %d random bytes", n, NodeKeyLen)
	}
	factory.addKnown(&node)
	return &node, nil
}

// ExistingNode adds a new node
func (factory *CryptNodeFactory) ExistingNode(name string, key [NodeKeyLen]byte) *CryptNode {
	var node CryptNode
	node.factory = factory
	node.Key = key
	node.name = name
	node.Mode = modeNotLoaded
	factory.addKnown(&node)
	return &node
}

func (factory *CryptNodeFactory) addKnown(node *CryptNode) {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if factory.known == nil {
		factory.known = make(map[[NodeKeyLen]byte]*CryptNode)
	}
	if _, ok := factory.known[node.Key]; ok {
		return
	}
	factory.known[node.Key] = node
	logger := log.WithField("key", fmt.Sprintf("%.10x", node.Key[:]))
	if node.name != "" {
		logger.WithField("name", node.name).Debug("Discovered node")
	} else {
		logger.Debug("Added node")
	}
}

func (factory *CryptNodeFactory) getKnown(key [NodeKeyLen]byte) *CryptNode {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if factory.known == nil {
		return nil
	}
	return factory.known[key]
}

// InvalidateCache invalidates a cached node
func (factory *CryptNodeFactory) InvalidateCache(mutation message.Message) {
	logger := log.WithFields(log.Fields{
		"op":       "import",
		"mutation": mutation.String(),
	})
	if len(mutation.Key()) != NodeKeyLen {
		logger.Debug("Not updating (not a metadata key)")
		return
	}
	var key [NodeKeyLen]byte
	copy(key[:], mutation.Key())
	node := factory.getKnown(key)
	if node == nil {
		logger.Debug("Not updating (unknown node)")
		return
	}
	node.mu.Lock()
	defer node.mu.Unlock()
	logger = logger.WithFields(log.Fields{
		"localVersion": node.version,
		"localName":    node.name,
	})
	if mutation.Version() <= node.version {
		logger.Debug("Not updating (stale update)")
		return
	}
	logger.Debug("Marking for update")
	node.shouldReloadMetadata = true
}
