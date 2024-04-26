// genesis.go is DEPRECATED

package metricsreporter

import (
	"strconv"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
)

type Genesis interface {
	GenesisMessageAccepted(role genesisspectypes.BeaconRole, round genesisspecqbft.Round)                // DEPRECATED
	GenesisMessageIgnored(reason string, role genesisspectypes.BeaconRole, round genesisspecqbft.Round)  // DEPRECATED
	GenesisMessageRejected(reason string, role genesisspectypes.BeaconRole, round genesisspecqbft.Round) // DEPRECATED
}

func (m *metricsReporter) GenesisMessageAccepted(
	role genesisspectypes.BeaconRole,
	round genesisspecqbft.Round,
) {
	messageValidationResult.WithLabelValues(
		messageAccepted,
		"",
		role.String(),
		strconv.FormatUint(uint64(round), 10),
	).Inc()
}

func (m *metricsReporter) GenesisMessageIgnored(
	reason string,
	role genesisspectypes.BeaconRole,
	round genesisspecqbft.Round,
) {
	messageValidationResult.WithLabelValues(
		messageIgnored,
		reason,
		role.String(),
		strconv.FormatUint(uint64(round), 10),
	).Inc()
}

func (m *metricsReporter) GenesisMessageRejected(
	reason string,
	role genesisspectypes.BeaconRole,
	round genesisspecqbft.Round,
) {
	messageValidationResult.WithLabelValues(
		messageRejected,
		reason,
		role.String(),
		strconv.FormatUint(uint64(round), 10),
	).Inc()
}
