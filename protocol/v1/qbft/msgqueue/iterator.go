package msgqueue

// IndexGenerator generates an index
type IndexGenerator func() Index

// IndexIterator enables to iterate over future created indices
type IndexIterator struct {
	generators []IndexGenerator
	i          int
}

// NewIndexIterator creates a new iterator
func NewIndexIterator() *IndexIterator {
	return &IndexIterator{
		generators: make([]IndexGenerator, 0),
		i:          0,
	}
}

// Add adds a new generator
func (ii *IndexIterator) Add(generators ...IndexGenerator) *IndexIterator {
	ii.generators = append(ii.generators, generators...)
	return ii
}

// Next returns the next generator
func (ii *IndexIterator) Next() IndexGenerator {
	if ii.i >= len(ii.generators) {
		return nil
	}
	next := ii.generators[ii.i]
	ii.i++
	return next
}

// Reset set iterator to 0.
// NOTE: use only in case we want to reuse the iterator
func (ii *IndexIterator) Reset() {
	ii.i = 0
}
