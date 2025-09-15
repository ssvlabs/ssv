package pebble

import (
	"errors"
	"fmt"
	"io"

	"github.com/cockroachdb/pebble"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

func getter(key []byte, dbFetch func(key []byte) ([]byte, io.Closer, error)) (basedb.Obj, bool, error) {
	value, closer, err := dbFetch(key)
	if errors.Is(err, pebble.ErrNotFound) {
		return basedb.Obj{}, false, nil
	}
	if err != nil {
		return basedb.Obj{}, true, err
	}

	// Since the returned value is only valid until closer.Close(),
	// we make a copy of it.
	val := make([]byte, len(value))
	copy(val, value)

	if err := closer.Close(); err != nil {
		return basedb.Obj{}, true, err
	}

	return basedb.Obj{
		Key:   key,
		Value: val,
	}, true, nil
}

func manyGetter(logger *zap.Logger, keys [][]byte, dbFetch func(key []byte) ([]byte, io.Closer, error), fn func(basedb.Obj) error) error {
	for _, key := range keys {
		value, closer, err := dbFetch(key)
		if err != nil {
			// If the key isn't found, skip it.
			if errors.Is(err, pebble.ErrNotFound) {
				logger.Debug("key not found", zap.String("key", string(key)))
				continue
			}
			return err
		}

		// Since the returned value is only valid until closer.Close(),
		// we make a copy of it.
		val := make([]byte, len(value))
		copy(val, value)

		// Close the closer to release resources.
		if err := closer.Close(); err != nil {
			return err
		}

		// Wrap the key/value in an object satisfying basedb.Obj.
		obj := basedb.Obj{
			Key:   key,
			Value: val,
		}

		// Call the iterator callback.
		if err := fn(obj); err != nil {
			return err
		}
	}

	return nil
}

func allGetter(iter *pebble.Iterator, prefix []byte, fn func(int, basedb.Obj) error) error {
	var i int
	for iter.First(); iter.Valid(); iter.Next() {
		value, err := iter.ValueAndErr()
		if err != nil {
			return fmt.Errorf("get value from db for key: %s: %w", string(iter.Key()), err)
		}

		// Since the returned key and value are only valid until the next
		// call of iter.Next() - we make a copy of it.
		key := make([]byte, len(iter.Key())-len(prefix))
		copy(key, iter.Key()[len(prefix):])

		val := make([]byte, len(value))
		copy(val, value)

		obj := basedb.Obj{
			Key:   key,
			Value: val,
		}

		// Call the iterator callback.
		if err := fn(i, obj); err != nil {
			return err
		}

		i++
	}

	return iter.Error()
}
