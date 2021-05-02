package storage

type Db interface {
	Set(prefix []byte, key []byte, value []byte) error
	Get(prefix []byte, key []byte) (Obj, error)
	GetAllByBucket(prefix []byte) ([]Obj, error)
}

// Obj struct for getting key/value from storage
type Obj struct {
	Key []byte
	Value []byte
}


