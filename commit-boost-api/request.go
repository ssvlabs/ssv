package cbapi

type RequestSignatureRequest struct {
	// ValidatorKeyType is which validator key (consensus, or proxy key) Ethereum
	// validator is asked to sign with
	ValidatorKeyType string `json:"type"`
	// ValidatorPubKeyHex is Ethereum validator public key - either consensus key (main key)
	// value or a proxy key value
	ValidatorPubKeyHex string `json:"pubkey"`
	// ObjectRootHex is pre-confirmation data Ethereum validator is asked to commit to
	ObjectRootHex string `json:"object_root"`
}
