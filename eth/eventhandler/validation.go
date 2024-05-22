package eventhandler

import (
	"fmt"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/herumi/bls-eth-go-binary/bls"

	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

const maxOperators = 13

func (eh *EventHandler) validateOperators(txn basedb.Txn, operators []uint64) error {
	operatorCount := len(operators)

	if operatorCount > maxOperators {
		return fmt.Errorf("too many operators (%d)", operatorCount)
	}

	if operatorCount == 0 {
		return fmt.Errorf("no operators")
	}

	if !ssvtypes.ValidCommitteeSize(len(operators)) {
		return fmt.Errorf("given operator count (%d) cannot build a 3f+1 quorum", operatorCount)
	}

	seenOperators := map[uint64]struct{}{}
	for _, operatorID := range operators {
		if _, ok := seenOperators[operatorID]; ok {
			return fmt.Errorf("duplicated operator ID (%d)", operatorID)
		}
		seenOperators[operatorID] = struct{}{}
	}

	exist, err := eh.nodeStorage.OperatorsExist(txn, operators)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}

	if !exist {
		return fmt.Errorf("not all operators exist")
	}

	return nil
}

// verify signature of the ValidatorAddedEvent shares data
// todo(align-contract-v0.3.1-rc.0): move to crypto package in ssv protocol?
func verifySignature(sig []byte, owner ethcommon.Address, pubKey []byte, nonce registrystorage.Nonce) error {
	data := fmt.Sprintf("%s:%d", owner.String(), nonce)
	hash := crypto.Keccak256([]byte(data))

	sign := &bls.Sign{}
	if err := sign.Deserialize(sig); err != nil {
		return fmt.Errorf("failed to deserialize signature: %w", err)
	}

	pk := &bls.PublicKey{}
	if err := pk.Deserialize(pubKey); err != nil {
		return fmt.Errorf("failed to deserialize public key: %w", err)
	}

	if res := sign.VerifyByte(pk, hash); !res {
		return fmt.Errorf("failed to verify signature")
	}

	return nil
}
