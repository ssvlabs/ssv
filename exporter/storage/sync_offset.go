package storage

import "github.com/bloxapp/ssv/eth1"

var (
	syncOffsetKey = []byte("syncOffset")
)

// SaveSyncOffset saves the offset
func (es *exporterStorage) SaveSyncOffset(offset *eth1.SyncOffset) error {
	return es.db.Set(storagePrefix, syncOffsetKey, offset.Bytes())
}

// GetSyncOffset returns the offset
func (es *exporterStorage) GetSyncOffset() (*eth1.SyncOffset, bool, error) {
	obj, found, err := es.db.Get(storagePrefix, syncOffsetKey)
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, found, err
	}
	offset := new(eth1.SyncOffset)
	offset.SetBytes(obj.Value)
	return offset, found, nil
}
