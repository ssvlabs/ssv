package codec

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/attestantio/go-eth2-client/api"
	apiv1bellatrix "github.com/attestantio/go-eth2-client/api/v1/bellatrix"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/pkg/errors"
)

// UnmarshalBlindedBlock unmarshals a versioned blinded beacon block from JSON or SSZ.
func UnmarshalBlindedBlock(contentType string, consensusVersion string, body io.Reader) (*api.VersionedSignedBlindedBeaconBlock, error) {
	signedBlindedBeaconBlock := &api.VersionedSignedBlindedBeaconBlock{}

	switch strings.ToLower(consensusVersion) {
	case "bellatrix":
		signedBlindedBeaconBlock.Version = spec.DataVersionBellatrix
		signedBlindedBeaconBlock.Bellatrix = &apiv1bellatrix.SignedBlindedBeaconBlock{}
	case "capella":
		signedBlindedBeaconBlock.Version = spec.DataVersionCapella
		signedBlindedBeaconBlock.Capella = &apiv1capella.SignedBlindedBeaconBlock{}
	case "deneb":
		signedBlindedBeaconBlock.Version = spec.DataVersionDeneb
		signedBlindedBeaconBlock.Deneb = &apiv1deneb.SignedBlindedBeaconBlock{}
	case "electra":
		signedBlindedBeaconBlock.Version = spec.DataVersionElectra
		signedBlindedBeaconBlock.Electra = &apiv1electra.SignedBlindedBeaconBlock{}
	case "fulu":
		signedBlindedBeaconBlock.Version = spec.DataVersionFulu
		signedBlindedBeaconBlock.Fulu = &apiv1electra.SignedBlindedBeaconBlock{}
	default:
		return nil, fmt.Errorf("unknown block version %v", consensusVersion)
	}

	switch strings.ToLower(contentType) {
	case "application/octet-stream":
		return unmarshalBlindedBlockSSZ(signedBlindedBeaconBlock, body)
	case "application/json":
		return unmarshalBlindedBlockJSON(signedBlindedBeaconBlock, body)
	default:
		return nil, fmt.Errorf("unsupported content type %s", contentType)
	}
}

func unmarshalBlindedBlockJSON(block *api.VersionedSignedBlindedBeaconBlock, body io.Reader) (*api.VersionedSignedBlindedBeaconBlock, error) {
	var err error
	switch block.Version {
	case spec.DataVersionBellatrix:
		err = json.NewDecoder(body).Decode(block.Bellatrix)
	case spec.DataVersionCapella:
		err = json.NewDecoder(body).Decode(block.Capella)
	case spec.DataVersionDeneb:
		err = json.NewDecoder(body).Decode(block.Deneb)
	case spec.DataVersionElectra:
		err = json.NewDecoder(body).Decode(block.Electra)
	case spec.DataVersionFulu:
		err = json.NewDecoder(body).Decode(block.Fulu)
	default:
		err = fmt.Errorf("unsupported block version %v", block.Version)
	}
	if err != nil {
		return nil, err
	}
	return block, nil
}

func unmarshalBlindedBlockSSZ(block *api.VersionedSignedBlindedBeaconBlock, body io.Reader) (*api.VersionedSignedBlindedBeaconBlock, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read body")
	}

	switch block.Version {
	case spec.DataVersionBellatrix:
		err = block.Bellatrix.UnmarshalSSZ(data)
	case spec.DataVersionCapella:
		err = block.Capella.UnmarshalSSZ(data)
	case spec.DataVersionDeneb:
		err = block.Deneb.UnmarshalSSZ(data)
	case spec.DataVersionElectra:
		err = block.Electra.UnmarshalSSZ(data)
	case spec.DataVersionFulu:
		err = block.Fulu.UnmarshalSSZ(data)
	default:
		err = fmt.Errorf("unsupported block version %v", block.Version)
	}
	if err != nil {
		return nil, err
	}

	return block, nil
}
