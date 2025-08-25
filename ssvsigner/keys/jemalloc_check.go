//go:build jemalloc

package keys

func init() {
	msg := "jemalloc is not supported because it clashes with openssl, please disable jemalloc by removing the build tags 'jemalloc, allocator' from the build command, e.g. 'go build -tags=\"allocator,jemalloc\"'"
	panic(msg)
}
