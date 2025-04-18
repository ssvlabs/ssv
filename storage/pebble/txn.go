package pebble

import (
	"errors"

	"github.com/cockroachdb/pebble"
	"github.com/ssvlabs/ssv/storage/basedb"
)

type pebbleTxn struct {
	batch *pebble.Batch
	db    *PebbleDB
}

func newPebbleTxn(batch *pebble.Batch, db *PebbleDB) basedb.Txn {
	return &pebbleTxn{
		batch: batch,
		db:    db,
	}
}

func (t *pebbleTxn) Commit() error {
	return t.batch.Commit(pebble.Sync)
}

func (t *pebbleTxn) Discard() {
	_ = t.batch.Close()
}

func (t *pebbleTxn) Set(prefix []byte, key []byte, value []byte) error {
	return t.batch.Set(append(prefix, key...), value, nil)
}

func (t *pebbleTxn) SetMany(prefix []byte, n int, next func(int) (basedb.Obj, error)) error {
	for i := range n {
		item, err := next(i)
		if err != nil {
			return err
		}
		if err := t.batch.Set(append(prefix, item.Key...), item.Value, nil); err != nil {
			return err
		}
	}
	return nil
}

func (t *pebbleTxn) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {
	value, closer, err := t.batch.Get(append(prefix, key...))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return basedb.Obj{}, false, nil
		}
		return basedb.Obj{}, true, err
	}

	valCopy := make([]byte, len(value))
	copy(valCopy, value)
	if err := closer.Close(); err != nil {
		return basedb.Obj{}, true, err
	}
	return basedb.Obj{
		Key:   key,
		Value: valCopy,
	}, true, nil
}

func (t *pebbleTxn) GetMany(prefix []byte, keys [][]byte, iterator func(basedb.Obj) error) error {
	for _, key := range keys {
		fullKey := append(prefix, key...)
		value, closer, err := t.batch.Get(fullKey)
		if err != nil {
			if errors.Is(err, pebble.ErrNotFound) {
				continue
			}
			return err
		}

		valCopy := make([]byte, len(value))
		copy(valCopy, value)
		if err := closer.Close(); err != nil {
			return err
		}
		obj := basedb.Obj{
			Key:   key,
			Value: valCopy,
		}
		if err := iterator(obj); err != nil {
			return err
		}
	}
	return nil
}

func (t *pebbleTxn) GetAll(prefix []byte, fn func(int, basedb.Obj) error) error {
	iter, err := makePrefixIter(t.batch, prefix)
	if err != nil {
		return err
	}

	defer func() { _ = iter.Close() }() // returns the same 'accumulated' error as iter.Error()

	i := 0
	// SeekPrefixGE starts prefix iteration mode.
	for iter.First(); iter.Valid(); iter.Next() {
		v, err := iter.ValueAndErr()
		if err != nil {
			continue
		}
		i++ // TODO: should we index including failed keys?

		key := make([]byte, len(iter.Key())-len(prefix))
		copy(key, iter.Key()[len(prefix):])

		val := make([]byte, len(v))
		copy(val, v)

		err = fn(i, basedb.Obj{
			Key:   key,
			Value: val,
		})
		if err != nil {
			return err
		}
	}

	return iter.Error()
}

func (t *pebbleTxn) Delete(prefix []byte, key []byte) error {
	return t.batch.Delete(append(prefix, key...), nil)
}
