package server

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/api/handlers"
	model "github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	dutytracer "github.com/ssvlabs/ssv/operator/dutytracer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
	"go.uber.org/zap"
)

func TestStandaloneServer(t *testing.T) {
	f, err := os.OpenFile("../../data/slot_3707881_3707882.ssz", os.O_RDONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}

	traces, err := readByteSlices(f)
	if err != nil {
		t.Fatal(err)
	}

	_ = f.Close()

	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	if err != nil {
		t.Fatal(err)
	}

	dutyStore := store.New(db)
	shares, vstore, _ := storage.NewSharesStorage(networkconfig.NetworkConfig{}, db, nil)

	tx := db.Begin()

	for _, share := range extractShares(traces) {
		shares.Save(tx, share)
	}

	_ = tx.Commit()

	logger := logging.TestLogger(t)

	collector := dutytracer.New(logger, vstore, mockDomainDataProvider{}, dutyStore, networkconfig.TestNetwork.BeaconConfig)

	for _, trace := range traces {
		go collector.Collect(t.Context(), trace, dummyVerify)
	}

	handler := handlers.NewExporter(logger, nil, collector, vstore)

	server := New(logger, "127.0.0.1:31602", nil, nil, handler, true)
	server.Run()
}

// normally we fetch shares from disk/beacon
// but for testing we just create fake shares from the messages
// and static data
func extractShares(messages []*queue.SSVMessage) []*types.SSVShare {
	shares := make([]*types.SSVShare, 0, len(messages))
	for _, msg := range messages {
		switch msg.MsgType {
		case spectypes.SSVPartialSignatureMsgType:
			if msg.MsgID.GetRoleType() != spectypes.RoleCommittee {
				pSigMessages := new(spectypes.PartialSignatureMessages)
				err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData())
				if err != nil {
					continue
				}
				if len(pSigMessages.Messages) == 0 {
					continue
				}
				var validatorPK spectypes.ValidatorPK
				copy(validatorPK[:], msg.MsgID.GetDutyExecutorID())
				share := &types.SSVShare{
					Status: eth2apiv1.ValidatorStateActiveExiting,
					Share: spectypes.Share{
						ValidatorIndex:  pSigMessages.Messages[0].ValidatorIndex,
						ValidatorPubKey: validatorPK,
					},
				}
				shares = append(shares, share)
			}
		}
	}

	for i, index := range missingIndices {
		var validatorPK spectypes.ValidatorPK
		copy(validatorPK[:], fmt.Sprintf("%d", i))

		share := &types.SSVShare{
			Status: eth2apiv1.ValidatorStateActiveExiting,
			Share: spectypes.Share{
				ValidatorIndex:  index,
				ValidatorPubKey: validatorPK,
			},
		}
		shares = append(shares, share)
	}

	return shares
}

func readByteSlices(file *os.File) (result []*queue.SSVMessage, err error) {
	var header [2]byte
	r := io.NewSectionReader(file, 0, math.MaxInt64)

	for {
		_, err := r.Read(header[:])
		if err == io.EOF {
			return result, nil
		}
		if err != nil {
			return nil, err
		}

		length := int(header[0])<<8 | int(header[1])

		diskMsgBytes := make([]byte, length)
		_, err = r.Read(diskMsgBytes)
		if err != nil {
			return nil, err
		}

		dataMsg := new(model.DiskMsg)
		if err := dataMsg.UnmarshalSSZ(diskMsgBytes); err != nil {
			return nil, err
		}

		msg := &queue.SSVMessage{
			SignedSSVMessage: &dataMsg.Signed,
			SSVMessage:       &dataMsg.Spec,
		}

		if dataMsg.Kind == 0 {
			msg.Body = &dataMsg.Qbft
		} else {
			msg.Body = &dataMsg.Sig
		}

		result = append(result, msg)
	}
}

type mockDomainDataProvider struct{}

func (m mockDomainDataProvider) DomainData(ctx context.Context, epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error) {
	return phase0.Domain{}, nil
}

func dummyVerify(*spectypes.PartialSignatureMessages) error {
	return nil
}

var missingIndices = []phase0.ValidatorIndex{
	1720473,
	1723722,
	1751588,
	1721936,
	1722781,
	1757487,
	1721486,
	1748551,
	1757320,
	1723061,
	1721144,
	1720835,
	1751914,
	1723989,
	1722842,
	1748705,
	1667176,
	1749889,
	1749349,
	1722927,
	1723779,
	1673798,
	1750000,
	1748115,
	1463759,
	1716892,
	1671808,
	1723826,
	1755748,
	1748461,
	1750924,
	1723905,
	1749925,
	1755822,
	1668755,
	1756152,
	1720382,
	1721428,
	1755855,
	1752853,
	1720833,
	1723203,
	1722624,
	1749405,
	1721083,
	1752647,
	1749273,
	1471821,
	1472136,
	1749075,
	1751990,
	1723884,
	1476362,
	1750539,
	1723364,
	1750844,
	1669398,
	1752497,
	1722907,
	1721415,
	1722580,
	1721731,
	1722158,
	1472173,
	1748696,
	1752902,
	1755712,
	1631689,
	1752546,
	1752204,
	1748174,
	1678456,
	1463691,
	1721059,
	1720805,
	1723438,
	1752747,
	1751458,
	1748149,
	1752132,
	1722004,
	1751422,
	1756425,
	1720797,
	1757550,
	1752711,
	1721612,
	1757317,
	1749604,
	1666859,
	1722636,
	1755812,
	1472140,
	1752246,
	1748219,
	1757682,
	1751818,
	1463592,
	1748873,
	1723188,
	1751246,
	1679908,
	1723320,
	1721130,
	1749963,
	1750700,
	1676174,
	1669933,
	1720385,
	1748342,
	1679227,
	1747956,
	1905916,
	1721280,
	1463596,
	1752467,
	1723958,
	1756469,
	1756170,
	1722914,
	1723976,
	1748289,
	1674198,
	1749529,
	1722595,
	1723612,
	1666284,
	1463403,
	1683707,
	1631498,
	1722133,
	1756424,
	1722397,
	1748807,
	1721337,
	1721637,
	1674165,
	1747915,
	1750647,
	1757325,
	1751438,
	1723259,
	1471815,
	1752235,
	1723035,
	1750705,
	1748972,
	1749083,
	1723865,
	1463470,
	1683184,
	1748882,
	1750003,
	1756176,
	1749218,
	1723066,
	1752247,
	1749801,
	1722296,
	1756193,
	1749789,
	1665643,
	1471928,
	1749094,
	1748975,
	1749629,
	1749906,
	1752253,
	1757371,
	1723842,
	1751600,
	1756454,
	1720730,
	1749667,
	1751493,
	1757640,
	1751254,
	1752721,
	1750891,
	1755968,
	1723073,
	1721856,
	1750570,
	1677310,
	1721404,
	1751734,
	1672297,
	1679919,
	1748227,
	1665390,
	1751520,
	1750787,
	1723172,
	1720874,
	1756570,
	1757193,
	1672219,
	1722891,
	1722054,
	1463521,
	1755816,
	1748886,
	1752120,
	1751934,
	1747909,
	1751307,
	1756500,
	1471531,
	1681446,
	1751052,
	1748273,
	1751926,
	1666783,
	1722223,
	1755989,
	1748125,
	1756391,
	1723775,
	1721924,
	1683189,
	1664181,
	1750758,
	1472032,
	1631435,
	1723201,
	1472260,
	1748624,
	1757443,
	1722098,
	1757610,
	1463322,
	1748816,
	1670176,
	1679164,
	1722442,
	1748181,
	1749180,
	1463580,
	1463726,
	1748832,
	1463528,
	1752261,
	1463423,
	1747882,
	1752148,
	1721377,
	1472172,
	1756045,
	1720314,
	1749300,
	1750030,
	1749186,
	1755861,
	1748827,
	1751860,
	1749914,
	1750287,
	1472118,
	1750234,
	1750015,
	1723564,
	1671511,
	1752620,
	1750808,
	1716800,
	1750176,
	1471896,
	1751465,
	1751066,
}
