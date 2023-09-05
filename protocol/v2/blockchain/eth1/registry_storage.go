package eth1

// RegistryStore interface for registry store
// TODO: extend this interface and re-think storage refactoring
type RegistryStore interface {
	DropRegistryData() error
}
