package records

import (
	crand "crypto/rand"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNodeRecord_Seal_Consume(t *testing.T) {
	netKey, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	require.NoError(t, err)
	enr := "enr:-N24QGUEylg7Y_58n_1CF6BWNy_G6joSotA0vjp9kx5hXAa_VayfO6BrIVJxEUg-9LH_R-_83Oq1Or7UkJiuQCo4NrWGAYCjk4RtgmlkgnY0gmlwhANlkICDb2lkuEBlYzFkYWI0OGNjNjgyZjNjYWVmODVkZjEzNDIwODRkMGU5ZGU3NzhjMDlmZTIzMjZiNzk1ZTEwY2M5MWVjMmQwiXNlY3AyNTZrMaEDH5jreBUoSVCgouwrcHtHIBu9yz41H8R54jt1FKklOjCDdGNwgjLIhHR5cGUBg3VkcIIu4A"
	pid, err := peer.IDFromPrivateKey(netKey)
	require.NoError(t, err)
	rec := NewNodeRecord("v0", pid.String(), "ec1dab48cc682f3caef85df1342084d0e9de778c09fe2326b795e10cc91ec2d0", enr)

	data, err := rec.Seal(netKey)
	require.NoError(t, err)
	parsedRec := &NodeRecord{}
	require.NoError(t, parsedRec.Consume(data))

	require.Equal(t, rec.OperatorID, parsedRec.OperatorID)
	require.Equal(t, rec.PeerID, parsedRec.PeerID)
}
