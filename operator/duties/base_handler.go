package duties

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/networkconfig"
)

// ExecuteDutiesFunc is a non-blocking functions which executes the given duties.
type ExecuteDutiesFunc func(logger *zap.Logger, duties []*spectypes.Duty)

type dutyHandler interface {
	Setup(BeaconNode, networkconfig.NetworkConfig, ValidatorController, ExecuteDutiesFunc, chan phase0.Slot, chan ReorgEvent, chan bool)
	HandleDuties(context.Context, *zap.Logger)
	Name() string
}

type baseHandler struct {
	beaconNode          BeaconNode
	network             networkconfig.NetworkConfig
	validatorController ValidatorController
	executeDuties       ExecuteDutiesFunc
	ticker              chan phase0.Slot

	reorg         chan ReorgEvent
	indicesChange chan bool

	fetchFirst     bool
	indicesChanged bool
}

func (h *baseHandler) Setup(
	beaconNode BeaconNode,
	network networkconfig.NetworkConfig,
	validatorController ValidatorController,
	executeDuties ExecuteDutiesFunc,
	ticker chan phase0.Slot,
	reorgEvents chan ReorgEvent,
	indicesChange chan bool,
) {
	h.beaconNode = beaconNode
	h.network = network
	h.validatorController = validatorController
	h.executeDuties = executeDuties
	h.ticker = ticker
	h.reorg = reorgEvents
	h.indicesChange = indicesChange
}

type Duties[K ~uint64, D any] struct {
	m map[K]map[phase0.Slot][]D
}

func NewDuties[K ~uint64, D any]() *Duties[K, D] {
	return &Duties[K, D]{
		m: make(map[K]map[phase0.Slot][]D),
	}
}

func (d *Duties[K, D]) Add(key K, slot phase0.Slot, duty D) {
	if _, ok := d.m[key]; !ok {
		d.m[key] = make(map[phase0.Slot][]D)
	}
	d.m[key][slot] = append(d.m[key][slot], duty)
}

func (d *Duties[K, D]) Reset(key K) {
	if _, ok := d.m[key]; ok {
		d.m[key] = make(map[phase0.Slot][]D)
	}
}
