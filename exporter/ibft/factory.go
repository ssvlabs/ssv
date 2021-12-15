package ibft

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/validator/storage"
	"sync"
)

// TODO: add to ibft package as most of parts here are code duplicates
// 		 tests should be added as well, currently it would cause redundant maintenance

var (
	decidedReaders sync.Map
	networkReaders sync.Map
)

// ShareHolder is an interface for components that hold a share
type ShareHolder interface {
	Share() *storage.Share
}

// Reader is an interface for ibft in the context of an exporter
type Reader interface {
	Start() error
	HandleMsg(msg *proto.SignedMessage)
}

// NewNetworkReader factory to create network readers
func NewNetworkReader(o IncomingMsgsReaderOptions) Reader {
	pk := o.PK.SerializeToHexStr()
	r, exist := networkReaders.Load(pk)
	if !exist {
		reader := newIncomingMsgsReader(o)
		networkReaders.Store(pk, reader)
		return reader
	}
	return r.(*incomingMsgsReader)
}

// NewDecidedReader factory to create decided readers
func NewDecidedReader(o DecidedReaderOptions) Reader {
	pk := o.ValidatorShare.PublicKey.SerializeToHexStr()
	r, exist := decidedReaders.Load(pk)
	if !exist {
		reader := newDecidedReader(o)
		decidedReaders.Store(pk, reader)
		return reader
	}
	return r.(*decidedReader)
}
