//go:build alan_spec

package spectest

// Identity shims for the alan_spec build. The DEFAULT (!alan_spec) build remaps a
// handful of error codes to reconcile our runner with the spec fixtures; the alan
// spec-test path performs no such remapping. These exist so the spectest package
// compiles under -tags alan_spec (the symbols are referenced unconditionally from
// the tag-free files). The alan spec-test suite itself is not wired up yet.
func adjustActualError(err error) error {
	return err
}

func adjustExpectedErrorCode(code int) int {
	return code
}
