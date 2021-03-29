package leader

type Selector interface {
	GetLeader() uint64
	BumpLeader() uint64
}
