package pebble

import (
	"errors"
	"io"

	"github.com/cockroachdb/pebble"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

type pFunc func(key []byte) ([]byte, io.Closer, error)

func getter(key []byte, pFunc pFunc) (basedb.Obj, bool, error) {
	value, closer, err := pFunc(key)
	if errors.Is(err, pebble.ErrNotFound) {
		return basedb.Obj{}, false, nil
	}
	if err != nil {
		return basedb.Obj{}, true, err
	}

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

func manyGetter(logger *zap.Logger, keys [][]byte, pFunc pFunc, fn func(basedb.Obj) error) error {
	for _, key := range keys {
		value, closer, err := pFunc(key)
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

func allGetter(logger *zap.Logger, iter *pebble.Iterator, prefix []byte, fn func(int, basedb.Obj) error) error {
	var i int
	for iter.First(); iter.Valid(); iter.Next() {
		value, err := iter.ValueAndErr()
		if err != nil {
			logger.Error("failed to get value", zap.Error(err), zap.String("key", string(iter.Key())))
			continue
		}

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
