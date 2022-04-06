package qbftstorage

type Iibft interface {
	DecidedMsgStore
	InstanceStore
}
