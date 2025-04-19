// Package ekm provides abstractions and implementations for managing validator
// shares, signing beacon chain objects, and applying slashing protection. It
// contains both local and remote key managers, a consistent storage mechanism
// for shares and slashing records, and helper utilities to ensure validators
// do not sign slashable data.
//
// See the individual implementations (LocalKeyManager, RemoteKeyManager)
// for more details on configuration and usage.
//
// This package also integrates with slashing protection from eth2-key-manager
// to prevent duplicate or conflicting signatures.

package ekm
