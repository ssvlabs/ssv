package gloas

import (
	"github.com/attestantio/go-eth2-client/spec"
)

// DataVersionGloas is a node-side placeholder for the Gloas beacon data version: until
// go-eth2-client defines it (its latest is Fulu), we slot Gloas immediately after Fulu.
// Remove and reconcile with upstream once it ships a real spec.DataVersionGloas. Note
// DataVersionGloas.String() returns "unknown" — the spec string/JSON tables aren't extended.
const DataVersionGloas = spec.DataVersionFulu + 1

// IsGloas reports whether the given beacon data version is Gloas (ePBS). For epoch-level
// fork gating use networkconfig (*Beacon).IsGloas instead.
func IsGloas(v spec.DataVersion) bool {
	return v == DataVersionGloas
}
