package storage

import (
	"fmt"
	"sync"
)

// InMemorySTore is a Store implementation powered by map, to be used for
// Testing or caches
type InMemorySTore struct {
	sync.Mutex
	m map[string][]byte
}

func NewInMemoryStore() *InMemorySTore {
	return &InMemorySTore{
		m: make(map[string][]byte),
	}
}

func (s *InMemorySTore) Put(key, value []byte) (err error) {
	s.Lock()
	s.m[string(key)] = dup(value)
	s.Unlock()
	return nil
}

func (s *InMemorySTore) Get(key []byte) (value []byte, err error) {
	s.Lock()
	value, ok := s.m[string(key)]
	s.Unlock()
	if !ok {
		return nil, fmt.Errorf("%.40q: %w", key, ErrNotFound)
	}
	if value == nil {
		value = []byte{}
	}
	return value, nil
}
