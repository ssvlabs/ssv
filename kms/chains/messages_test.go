package chains

import (
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// Expected digests were computed with viem (hashMessage, hashTypedData, hashDomain).

const siweMessage = "app.example.com wants you to sign in with your Ethereum account:\n0x1111111111111111111111111111111111111111\n\nSign in.\n\nURI: https://app.example.com\nVersion: 1\nChain ID: 42161\nNonce: abcdef12\nIssued At: 2026-10-16T00:00:00Z"

func msgPayload(t *testing.T, m string) []byte {
	b, err := json.Marshal(EIP191Message{Message: m})
	require.NoError(t, err)
	return b
}

func TestEIP191(t *testing.T) {
	for _, ok := range []string{"eip191:0", "eip191:1", "eip191:42161", "eip712:0", "eip712:1337"} {
		_, _, err := ParseID(ok)
		require.NoError(t, err, ok)
	}
	for _, bad := range []string{"eip191:01", "eip191:-1", "eip712:x"} {
		_, _, err := ParseID(bad)
		require.Error(t, err, bad)
	}

	_, tx, err := Parse("eip191:42161", msgPayload(t, siweMessage))
	require.NoError(t, err)
	require.Equal(t, "0x2e1e1aaa4d76f32b4671f57dc871617ea67287b93b5ce3c041954ea6424cf56a", hexutil.Encode(tx.Digest()))
	pf := tx.Policy()
	require.Equal(t, []byte("app.example.com"), pf.Target)
	require.Empty(t, pf.Data)
	require.Zero(t, pf.Value.Sign())
	_, seq := tx.Sequence()
	require.False(t, seq)

	_, err = EIP191{}.Parse("1", msgPayload(t, siweMessage))
	require.ErrorContains(t, err, "does not match chain eip191:1")

	_, plain, err := Parse("eip191:0", msgPayload(t, "hello world"))
	require.NoError(t, err)
	require.Equal(t, "0xd9eba16ed0ecae432b71fe008c98cc872bb4cc214d3220a36f365326cf807d68", hexutil.Encode(plain.Digest()))
	require.Empty(t, plain.Policy().Target)
	require.Equal(t, []byte("hello world"), plain.Policy().Data)
	_, err = EIP191{}.Parse("1", msgPayload(t, "hello world"))
	require.ErrorContains(t, err, "eip191:0")

	// A scheme on the first line and a port in the domain.
	d, c, ok := SIWE("https://localhost:3000 wants you to sign in with your Ethereum account:\n0x11\n")
	require.True(t, ok)
	require.Equal(t, "localhost:3000", d)
	require.Equal(t, "", c)

	// The signature comes back with V as 27/28 and recovers to the signer.
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sig, err := crypto.Sign(tx.Digest(), key)
	require.NoError(t, err)
	raw, hash, err := tx.Assemble(sig)
	require.NoError(t, err)
	require.Equal(t, hexutil.Encode(tx.Digest()), hash)
	require.Contains(t, []byte{27, 28}, raw[64])
	require.True(t, IsOffchain(EIP191{}))
	require.False(t, IsOffchain(EVM{}))
}

const approveAgent = `{
  "domain": {"name": "HyperliquidSignTransaction", "version": "1", "chainId": 42161, "verifyingContract": "0x0000000000000000000000000000000000000000"},
  "types": {"HyperliquidTransaction:ApproveAgent": [
    {"name": "hyperliquidChain", "type": "string"}, {"name": "agentAddress", "type": "address"},
    {"name": "agentName", "type": "string"}, {"name": "nonce", "type": "uint64"}]},
  "primaryType": "HyperliquidTransaction:ApproveAgent",
  "message": {"hyperliquidChain": "Mainnet", "agentAddress": "0x2222222222222222222222222222222222222222", "agentName": "friend", "nonce": 1760572800000}
}`

func TestEIP712(t *testing.T) {
	_, tx, err := Parse("eip712:42161", []byte(approveAgent))
	require.NoError(t, err)
	require.Equal(t, "0x52734fb300ea22720790f4f574d71c9c9bd0193ec38ca7d5859dabbaf6b558ad", hexutil.Encode(tx.Digest()))
	require.Equal(t,
		"0x2f54e643b38c9c2b65cb731aaf11c54f01705ee0cdf45aea16e2ac0a5ce7531a"+"7a5bc85e3b3498d7565fc235846e6c77889fc41f8bf7435238d357155a0f7071",
		hexutil.Encode(tx.Policy().Target))
	require.Empty(t, tx.Policy().Data)

	_, err = EIP712{}.Parse("1", []byte(approveAgent))
	require.ErrorContains(t, err, "does not match chain eip712:1")

	// The same with EIP712Domain spelled out, and the chain ID as a string: same digest.
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(approveAgent), &m))
	m["types"].(map[string]any)["EIP712Domain"] = []map[string]string{
		{"name": "name", "type": "string"}, {"name": "version", "type": "string"},
		{"name": "chainId", "type": "uint256"}, {"name": "verifyingContract", "type": "address"},
	}
	m["domain"].(map[string]any)["chainId"] = "42161"
	m["message"].(map[string]any)["nonce"] = "1760572800000"
	b, _ := json.Marshal(m)
	_, tx2, err := Parse("eip712:42161", b)
	require.NoError(t, err)
	require.Equal(t, tx.Digest(), tx2.Digest())

	// Integers above 2^53 keep full precision; a domain without chainId uses eip712:0.
	big := `{"domain":{"name":"Big"},"types":{"M":[{"name":"v","type":"uint256"}]},"primaryType":"M","message":{"v":123456789012345678901234567890}}`
	_, tx3, err := Parse("eip712:0", []byte(big))
	require.NoError(t, err)
	require.Equal(t, "0x13108675e8e32c2f0386dfce049b5c0d3fb46533ae03b269b63a5f4068d18050", hexutil.Encode(tx3.Digest()))

	for _, bad := range []string{
		`{"domain":{"name":"x"},"types":{"M":[]},"primaryType":"N","message":{}}`,
		`{"domain":{"name":"x"},"types":{"EIP712Domain":[]},"primaryType":"EIP712Domain","message":{}}`,
		`not json`,
	} {
		_, _, err := Parse("eip712:0", []byte(bad))
		require.Error(t, err, bad)
	}
}
