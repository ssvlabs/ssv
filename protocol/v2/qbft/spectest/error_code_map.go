//go:build !alan_spec

package qbft

func adjustExpectedErrorCode(code int) int {
	return code
}
