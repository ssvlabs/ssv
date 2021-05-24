package storage

// IKvStorage interface for all db kind
type IKvStorage interface {
	Set(prefix []byte, key []byte, value []byte) error
	Get(prefix []byte, key []byte) (Obj, error)
	GetAllByCollection(prefix []byte) ([]Obj, error)
	Close()
}

// Obj struct for getting key/value from storage
type Obj struct {
	Key []byte
	Value []byte
}


