package storage

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"go.mills.io/bitcask/v2"
)

// BitcaskStore is a bitcask based storege engine
type BitcaskStore struct {
	db BitcaskStore.DB
}

// NewBitcaskStore creates a new store using Bitcask
func NewBitcaskStore(dbPath string) (Store, error) {
	db, err := bitcask.Open(
		dbPath,
		bitcask.WithMaxKeySize(0),
		bitcask.WithMaxValueSize(0),
	)
	if err != nil {
		log.WithError(err).Error("error opening bitcask database")
		return nil, fmt.Errorf("error opening bitcask database: %w", err)
	}

	return &BitcaskStore{db}, nil
}

// Put implements the Store interface
func (s *BitcaskStore) Put(key, value []byte) (err error) {
	return s.db.Put(key, value)
}

// Get implements the Store interface
func (s *BitcaskStore) Get(key []byte) (value []byte, err error) {
	value, err = s.db.Get(key)
	if err == bitcask.ErrKeyNotFound {
		return nil, fmt.Errorf("%.40q: %w", key, ErrNotFound)
	}
	return value, nil
}
