package node

import (
	"bytes"
	"errors"
	"syscall"
	"time"

	"github.com/EncrypteDL/CryptFS/pkg/bits"
	"github.com/EncrypteDL/CryptFS/pkg/storage"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	log "github.com/sirupsen/logrus"
)

func (node *CryptNode) serialize() []byte {
	// Could use a pool of buffers, to be reused, instead of putting pressure on
	// the GC.
	size := 24 + len(node.contentKey)
	for attr, value := range node.xattrs {
		size += 4 + len(attr) + len(value)
	}
	for childName := range node.Children {
		size += 4 + NodeKeyLen + len(childName)
	}
	buf := make([]byte, size)
	b := buf
	b = bits.Put32(b, node.User)
	b = bits.Put32(b, node.Group)
	b = bits.Put32(b, node.Mode)
	b = bits.Put64(b, uint64(node.Time.UnixNano()))
	b = bits.Putb(b, node.contentKey)
	b = bits.Put16(b, uint16(len(node.xattrs)))
	for attr, value := range node.xattrs {
		b = bits.Puts(b, attr)
		b = bits.Putb(b, value)
	}
	for childName, childNode := range node.Children {
		b = bits.Puts(b, childName)
		b = bits.Putb(b, childNode.Key[:])
	}
	return buf
}

func (node *CryptNode) unserialize(b []byte) {
	node.User, b = bits.Get32(b)
	node.Group, b = bits.Get32(b)
	node.Mode, b = bits.Get32(b)
	var unixnano uint64
	unixnano, b = bits.Get64(b)
	node.Time = time.Unix(0, int64(unixnano))
	node.contentKey, b = bits.Getb(b)
	if node.Mode&fuse.S_IFDIR != 0 {
		node.Children = make(map[string]*CryptNode)
	}
	var nxattr uint16
	nxattr, b = bits.Get16(b)
	if nxattr > 0 {
		node.xattrs = make(map[string][]byte)
	}
	for ; nxattr > 0; nxattr-- {
		var attr string
		var value []byte
		attr, b = bits.Gets(b)
		value, b = bits.Getb(b)
		node.xattrs[attr] = value
	}
	if len(b) > 0 {
		var childName string
		var childKey []byte
		for len(b) > 0 {
			childName, b = bits.Gets(b)
			childKey, b = bits.Getb(b)
			var key [NodeKeyLen]byte
			copy(key[:], childKey)
			node.Children[childName] = node.factory.ExistingNode(childName, key)
		}
	}
}

func (node *CryptNode) saveMetadata() error {
	value := node.serialize()
	err := node.factory.Metadata.Put(node.version+1, node.Key[:], value)
	if err != nil {
		return err
	}
	node.version++
	return nil
}

// LoadMetadata loads metadata for a node
func (node *CryptNode) LoadMetadata(key [NodeKeyLen]byte) error {
	version, b, err := node.factory.Metadata.Get(key[:])
	if err != nil {
		return err
	}
	node.Key = key
	node.version = version
	node.unserialize(b)
	return nil
}

func (node *CryptNode) sync() syscall.Errno {
	if node.shouldSaveContent {
		var err error
		prev := node.contentKey
		node.contentKey, err = node.factory.Blobs.Put(node.content)
		if err != nil {
			log.WithFields(log.Fields{
				"err": err,
			}).Error("Could not save content")
			return syscall.EIO
		}
		node.shouldSaveContent = false
		if !bytes.Equal(prev, node.contentKey) {
			node.shouldSaveMetadata = true
		}
	}
	if node.shouldSaveMetadata {
		err := node.saveMetadata()
		if err != nil {
			if errors.Is(err, storage.ErrStalePut) {
				node.shouldReloadMetadata = true
			}
			log.WithFields(log.Fields{
				"err": err,
			}).Error("Could not save metadata")
			return syscall.EIO
		}
		node.shouldSaveMetadata = false
	}
	return fs.OK
}
