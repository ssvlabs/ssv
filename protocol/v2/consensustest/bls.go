package consensustest

import (
	"fmt"

	"github.com/herumi/bls-eth-go-binary/bls"

	"github.com/ssvlabs/ssv/utils/threshold"
)

// GenerateBLSKeys produces a fresh threshold-shared BLS keypair at q = 2f+1.
// Pass the result to SimConfig.BLSKeys to opt into real BLS for adapters that
// support it (currently OBFT). Initializes herumi/bls process-globally;
// reuse the result across sims since generation is slow.
func GenerateBLSKeys(operators []OperatorID) (*BLSKeys, error) {
	threshold.Init()
	n := len(operators)
	if n < 4 {
		return nil, fmt.Errorf("consensustest: BLSKeys needs at least n=4 operators (got %d)", n)
	}
	if (n-1)%3 != 0 {
		return nil, fmt.Errorf("consensustest: BLSKeys needs n = 3f+1 (got %d)", n)
	}
	f := (n - 1) / 3
	q := 2*f + 1

	master := &bls.SecretKey{}
	master.SetByCSPRNG()

	shares, err := threshold.Create(master.Serialize(), uint64(q), uint64(n)) //nolint:gosec // small positive
	if err != nil {
		return nil, fmt.Errorf("consensustest: threshold.Create: %w", err)
	}

	out := &BLSKeys{
		ClusterPubKey: master.GetPublicKey().Serialize(),
		Shares:        make(map[OperatorID][]byte, n),
		PubShares:     make(map[OperatorID][]byte, n),
	}
	for _, op := range operators {
		sk, ok := shares[uint64(op)]
		if !ok {
			return nil, fmt.Errorf("consensustest: missing share for op %d", op)
		}
		out.Shares[op] = sk.Serialize()
		out.PubShares[op] = sk.GetPublicKey().Serialize()
	}
	return out, nil
}
