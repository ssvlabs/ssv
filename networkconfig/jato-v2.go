package networkconfig

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
)

var JatoV2 = func() spectypes.BeaconNetwork {
	cfg := Jato
	cfg.Name = "jato-v2"
	cfg.SSV.Domain = [4]byte{0x00, 0x00, 0x30, 0x12}

	return cfg
}()
