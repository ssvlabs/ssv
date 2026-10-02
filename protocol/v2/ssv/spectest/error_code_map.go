//go:build !alan_spec

package spectest

// adjustExpectedErrorCode is identity in the default build; the alan_spec build remaps
// v1.2.2 fixture error codes in error_code_map_alan.go.
func adjustExpectedErrorCode(code int) int {
	return code
}
