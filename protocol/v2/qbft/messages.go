package qbft

import "crypto/sha256"

// HashDataRoot hashes input data to root.
func HashDataRoot(data []byte) [32]byte {
	return sha256.Sum256(data)
}
