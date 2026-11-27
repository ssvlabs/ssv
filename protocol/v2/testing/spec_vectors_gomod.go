//go:build !alan_spec

package testing

// specGoModFilename names the module file that pins the ssv-spec version whose vectors the spec tests run. The
// alan_spec build reads go.spec.alan.mod instead, to run the alan (pre-Boole) release's vectors.
func specGoModFilename() string {
	return "go.mod"
}
