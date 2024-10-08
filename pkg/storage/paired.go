package storage

import (
	"errors"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
)

// Paired implements Store wrapping a pair of stores, one fast, one slow. It
// will handle puts storing data in the fast store and syncing that to the slow
// store in the background. It will handle gets from the fast store if possible,
// otherwise from the slow store (and in this case also propagate the data from
// the slow to the fast store, for next time that piece of data is requested).
type Paired struct {
	fast Store
	slow Store

	wbc chan [2][]byte
}

func NewPaired(fast, slow Store) Paired {
	p := Paired{
		fast: fast,
		slow: slow,
		wbc:  make(chan [2][]byte, 42),
	}

	//Exits only when the process is terminited
	go p.writeback()
	return p
}

func (s Paired) Get(Key []byte) (value []byte, err error) {
	value, err = s.fast.Get(Key)
	if err != nil {
		return
	}
	if !errors.Is(err, ErrNotFound) {
		return
	}

	value, err = s.slow.Get(Key)
	if err != nil {
		return nil, err
	}

	logger := log.WithFields(log.Fields{
		"key": fmt.Sprintf("%.10x", Key),
	})
	if ferr := s.fast.Put(Key, value); ferr != nil {
		logger.WithField("err", ferr).Warn("Could not propagate from slow to fast")
	} else {
		logger.Debug("Propagated from slow to fast")
	}
	return value, nil
}

func (s Paired) writeback() {
	for kv := range s.wbc {
		key := kv[0]
		value := kv[1]
		s.writeback1(key, value)
	}
}

func (s Paired) writeback1(key, value []byte) {
	logger := log.WithFields(log.Fields{
		"key": fmt.Sprintf("%.10x", key),
	})
	for {
		err := s.slow.Put(key, value)
		if err == nil {
			logger.Debug("Propagated from fast to slow")
			break
		}
		logger.WithFields(log.Fields{
			"err": err,
		}).Warn("Could not propagate from fast to slow")
		// Should randomize.
		time.Sleep(time.Second)
	}
}

func dup(p []byte) []byte {
	q := make([]byte, len(p))
	copy(q, p)
	return q
}
