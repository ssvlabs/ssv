package ibft

import "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"

//import (
//	"sync"
//
//	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
//)
//
//// TODO: add to ibft package as most of parts here are code duplicates
//// 		 tests should be added as well, currently it would cause redundant maintenance
//
//var (
//	decidedReaders sync.Map
//	networkReaders sync.Map
//)
//
// ShareHolder is an interface for components that hold a share
type ShareHolder interface {
	Share() *beacon.Share
}

// Reader is an interface for ibft in the context of an exporter
type Reader interface {
	Start() error
}

//
//// NewNetworkReader factory to create network readers
//func NewNetworkReader(o IncomingMsgsReaderOptions) Reader {
//	pk := o.PK.SerializeToHexStr()
//	r, exist := networkReaders.Load(pk)
//	if !exist {
//		reader := newIncomingMsgsReader(o)
//		networkReaders.Store(pk, reader)
//		return reader
//	}
//	return r.(*incomingMsgsReader)
//}
//
//// NewDecidedReader factory to create decided readers
//func NewDecidedReader(o DecidedReaderOptions) Reader {
//	pk := o.ValidatorShare.PublicKey.SerializeToHexStr()
//	r, exist := decidedReaders.Load(pk)
//	if !exist {
//		reader := newDecidedReader(o)
//		decidedReaders.Store(pk, reader)
//		return reader
//	}
//	return r.(*decidedReader)
//}
