package smr

// Block represents a block in the SMR chain.
//
// For TC locking we only need ancestry (Parent) plus basic ordering (View, Height).
// Root is used as a stable identifier for signing/verification of timeout messages.
type Block struct {
	View   uint64
	Height uint64
	Root   [32]byte
	Parent *Block
}

// BlockExtends returns true if child extends (or equals) parent.
//
// A nil parent is treated as the "genesis" and is extended by all blocks (including nil).
func BlockExtends(child, parent *Block) bool {
	if parent == nil {
		return true
	}
	for cur := child; cur != nil; cur = cur.Parent {
		if cur == parent {
			return true
		}
	}
	return false
}

// BlocksConflict returns true if neither block extends the other.
//
// A nil block never conflicts.
func BlocksConflict(a, b *Block) bool {
	if a == nil || b == nil {
		return false
	}
	return !BlockExtends(a, b) && !BlockExtends(b, a)
}

// HighestBlock returns the highest block by (View, Height).
func HighestBlock(blocks []*Block) *Block {
	var best *Block
	for _, b := range blocks {
		if b == nil {
			continue
		}
		if best == nil || b.View > best.View || (b.View == best.View && b.Height > best.Height) {
			best = b
		}
	}
	return best
}
