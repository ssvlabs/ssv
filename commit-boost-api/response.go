package cbapi

type GetPubKeysResponse struct {
	Keys []ValidatorKeys `json:"keys"`
}

type ValidatorKeys struct {
	// Consensus is the main Ethereum validator key
	Consensus string `json:"consensus"`
	// ProxyBLS are BLS keys derived from Consensus key
	ProxyBLS []string `json:"proxy_bls"`
	// ProxyECDSA are ECDSA keys derived from Consensus key
	ProxyECDSA []string `json:"proxy_ecdsa"`
}

type RequestSignatureResponse struct {
	Signature string `json:"signature"`
}

type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
