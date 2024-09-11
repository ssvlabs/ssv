// package discover is copied from github.com/ethereum/go-ethereum/p2p/discover@v1.13.15.
// The reason for copying is the need to use peer filter when looking for peers based on domain.
// As the amount of changes is small, necessary files were copied instead of forking.
// The files in this package need to be updated if go-ethereum version changes.

package discover
