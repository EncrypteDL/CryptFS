package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

// DiskStore implement Store
type DiskStore struct {
	dir string
}

// NewDiskStore contructs a new Disk backed store
func NewDiskStore(dir string) *DiskStore {
	return &DiskStore{dir: dir}
}

// Put implements the BlobStore interface
func (s *DiskStore) Put(key, value []byte) (err error) {
	p := s.pathFor(key)
	err = os.WriteFile(p, value, 0600)
	if err != nil {
		return nil
	}

	if !os.IsNotExist(err) {
		return fmt.Errorf("could not write %q: %w", p, err)
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return fmt.Errorf("could not make dir for %q: %w", p, err)
	}
	return os.WriteFile(p, value, 0600)
}

// Get implements the BlobStore interface
func (s *DiskStore) Get(key []byte) (value []byte, err error) {
	value, err = os.ReadFile(s.pathFor(key))
	if os.IsNotExist(err) {
		err = fmt.Errorf("%x: %w", key, ErrNotFound)
	}
	return
}

func (s *DiskStore) pathFor(key []byte) string {
	hex := fmt.Sprintf("%02x", key)
	return filepath.Join(s.dir, hex[:2], hex)
}
