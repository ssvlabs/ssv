package chains

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestParseID(t *testing.T) {
	for _, ok := range []string{"eip155:1", "eip155:8453", "eip155:11155111"} {
		_, _, err := ParseID(ok)
		require.NoError(t, err, ok)
	}
	for _, bad := range []string{"", "evm", "eip155:", "eip155:0", "eip155:01", "eip155:x", "EIP155:1", "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"} {
		_, _, err := ParseID(bad)
		require.Error(t, err, bad)
	}
}

func TestEVMTxRoundTrip(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	payload, _ := json.Marshal(EVMTx{ChainID: 8453, Nonce: 7, To: &to, Value: "1000", Gas: 21000, MaxFeePerGas: "100", MaxPriorityFeePerGas: "1"})

	_, err = EVM{}.Parse("1", payload)
	require.ErrorContains(t, err, "does not match chain eip155:1")

	id, tx, err := Parse("eip155:8453", payload)
	require.NoError(t, err)
	require.Equal(t, "eip155:8453", id.String())
	seq, ok := tx.Sequence()
	require.True(t, ok)
	require.Equal(t, uint64(7), seq)
	pf := tx.Policy()
	require.Equal(t, to.Bytes(), pf.Target)
	require.Equal(t, big.NewInt(1000), pf.Value)

	sig, err := crypto.Sign(tx.Digest(), key)
	require.NoError(t, err)
	raw, hash, err := tx.Assemble(sig)
	require.NoError(t, err)
	var signed ethtypes.Transaction
	require.NoError(t, signed.UnmarshalBinary(raw))
	from, err := ethtypes.LatestSignerForChainID(big.NewInt(8453)).Sender(&signed)
	require.NoError(t, err)
	require.Equal(t, crypto.PubkeyToAddress(key.PublicKey), from)
	require.Equal(t, signed.Hash().Hex(), hash)

	// The same transaction on another chain has another digest: no cross-chain replay.
	other, _ := json.Marshal(EVMTx{ChainID: 10, Nonce: 7, To: &to, Value: "1000", Gas: 21000, MaxFeePerGas: "100", MaxPriorityFeePerGas: "1"})
	_, tx10, err := Parse("eip155:10", other)
	require.NoError(t, err)
	require.NotEqual(t, tx.Digest(), tx10.Digest())
	require.NotEqual(t, ID{"eip155", "8453"}.Hash(), ID{"eip155", "10"}.Hash())
}
