package storage

import "golang.org/x/crypto/blake2b"

// BlobStore is the interface for storing and retrieving blogs of data
type BlobStore interface {
	Get(key []byte) (value []byte, err error)
	Put(value []byte) (key []byte, err error)
}

// BlobStoreWrapper wraps a Store to make sure content is never overwritten, by using
// as key for a value the Blake2b hash of the value. Even if there are concurrent
// writes for the same key, those would write the same contents (with very high
// probability).
type BlobStoreWrapper struct {
	delegate Store
}

// NewBlobStore creates a new blob store with the provided delegate store
func NewBlobStore(delegate Store) *BlobStoreWrapper {
	return &BlobStoreWrapper{
		delegate: delegate,
	}
}

// Put implements the BlobStore interface
func (s *BlobStoreWrapper) Put(value []byte) (key []byte, err error) {
	hash := blake2b.Sum512(value)
	key = hash[:]
	err = s.delegate.Put(key, value)
	return
}

// Get implements the BlobStore interface
func (s *BlobStoreWrapper) Get(key []byte) (value []byte, err error) {
	return s.delegate.Get(key)
}
