package validator

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"

	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

const maxOperators = 13

func validateOperators(operators []uint64) error {
	operatorCount := len(operators)

	if operatorCount > maxOperators {
		return fmt.Errorf("too many operators (%d)", operatorCount)
	}

	if operatorCount == 0 {
		return fmt.Errorf("no operators")
	}

	canBeQuorum := func(v int) bool {
		return (v-1)%3 == 0 && (v-1)/3 != 0
	}

	if !canBeQuorum(len(operators)) {
		return fmt.Errorf("given operator count (%d) cannot build a 3f+1 quorum", operatorCount)
	}

	seenOperators := map[uint64]struct{}{}
	for _, operatorID := range operators {
		if _, ok := seenOperators[operatorID]; ok {
			return fmt.Errorf("duplicated operator ID (%d)", operatorID)
		}
		seenOperators[operatorID] = struct{}{}
	}

	return nil
}

// todo(align-contract-v0.3.1-rc.0): move to crypto package in ssv protocol?
// verify signature of the ValidatorAddedEvent shares data
func verifySignature(sig []byte, owner common.Address, pubKey []byte, nonce registrystorage.Nonce) error {
	data := fmt.Sprintf("%s:%d", owner.String(), nonce)
	hash := crypto.Keccak256([]byte(data))

	sign := &bls.Sign{}
	if err := sign.Deserialize(sig); err != nil {
		return errors.Wrap(err, "failed to deserialize signature")
	}

	pk := &bls.PublicKey{}
	if err := pk.Deserialize(pubKey); err != nil {
		return errors.Wrap(err, "failed to deserialize public key")
	}

	if res := sign.VerifyByte(pk, hash); !res {
		return errors.New("failed to verify signature")
	}

	return nil
}
