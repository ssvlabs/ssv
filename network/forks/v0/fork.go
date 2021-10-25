package v0

// ForkV0 is the genesis version 0 implementation
type ForkV0 struct {
}

// New returns an instance of ForkV0
func New() *ForkV0 {
	return &ForkV0{}
}

// SlotTick implementation
func (v0 *ForkV0) SlotTick(slot uint64) {

}
