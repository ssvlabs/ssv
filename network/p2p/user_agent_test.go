package p2p

import (
	"github.com/bloxapp/ssv/utils/commons"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestUserAgent(t *testing.T) {
	commons.SetBuildData("ssvtest", "v0.x.x")
	_, skBytes, err := rsaencryption.GenerateKeys()
	require.NoError(t, err)
	require.NotNil(t, skBytes)
	sk, err := rsaencryption.ConvertPemToPrivateKey(string(skBytes))
	require.NoError(t, err)

	t.Run("generate valid ua", func(t *testing.T) {
		ua, err := GenerateUserAgent(sk, Exporter)
		require.NoError(t, err)
		require.Equal(t, 0, strings.Index(string(ua), "ssvtest:v0.x.x:exporter:"))

		ua, err = GenerateUserAgent(sk, Operator)
		require.NoError(t, err)
		require.Equal(t, 0, strings.Index(string(ua), "ssvtest:v0.x.x:operator:"))
	})

	t.Run("generate w/o operator key", func(t *testing.T) {
		ua, err := GenerateUserAgent(nil, Operator)
		require.NoError(t, err)
		require.Equal(t, "ssvtest:v0.x.x:operator", string(ua))
	})

	t.Run("get node type", func(t *testing.T) {
		ua, err := GenerateUserAgent(sk, Operator)
		require.NoError(t, err)
		require.Equal(t, Operator.String(), ua.NodeType())

		ua, err = GenerateUserAgent(sk, Unknown)
		require.NoError(t, err)
		require.Equal(t, Unknown.String(), ua.NodeType())
	})

	t.Run("get node version", func(t *testing.T) {
		ua, err := GenerateUserAgent(sk, Operator)
		require.NoError(t, err)
		require.Equal(t, "v0.x.x", ua.NodeVersion())
	})

	t.Run("get node pubKey hash", func(t *testing.T) {
		ua, err := GenerateUserAgent(sk, Operator)
		require.NoError(t, err)
		require.Equal(t, 64, len(ua.OperatorID()))
	})

	t.Run("get node pubKey hash, no operator key", func(t *testing.T) {
		ua, err := GenerateUserAgent(nil, Operator)
		require.NoError(t, err)
		require.Equal(t, 0, len(ua.OperatorID()))
	})
}
