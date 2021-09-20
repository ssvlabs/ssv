package beacon

import (
	"encoding/hex"
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func FetchValidators(bc Beacon, pubKeys [][]byte) (map[string]*v1.Validator, error) {
	logger := logex.GetLogger(zap.String("func", "FetchValidators"))
	if len(pubKeys) == 0 {
		return nil, nil
	}
	var pubkeys []phase0.BLSPubKey
	for _, pk := range pubKeys {
		blsPubKey := phase0.BLSPubKey{}
		copy(blsPubKey[:], pk)
		pubkeys = append(pubkeys, blsPubKey)
	}
	logger.Debug("fetching indices for public keys", zap.Int("total", len(pubkeys)),
		zap.Any("pubkeys", pubkeys))
	validatorsIndexMap, err := bc.GetValidatorData(pubkeys)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get indices from beacon")
	}
	ret := make(map[string]*v1.Validator)
	for index, v := range validatorsIndexMap {
		pk := hex.EncodeToString(v.Validator.PublicKey[:])
		ret[pk] = v
		// once fetched, the internal map in go-client should be updated
		bc.ExtendIndexMap(index, v.Validator.PublicKey)
	}
	return ret, nil
}
