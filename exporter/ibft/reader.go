package ibft

// Reader is a minimal interface for ibft in the context of an exporter
type Reader interface {
	Start() error
}
