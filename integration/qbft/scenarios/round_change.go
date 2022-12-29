package scenarios

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/altair"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"

	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
)

func RoundChange(role spectypes.BeaconRole) *IntegrationTest {
	pk := spectestingutils.Testing4SharesSet().ValidatorPK.Serialize()
	identifier := spectypes.NewMsgID(pk, role)

	slots := []spec.Slot{
		spec.Slot(spectestingutils.TestingDutySlot + 0),
		spec.Slot(spectestingutils.TestingDutySlot + 1),
		spec.Slot(spectestingutils.TestingDutySlot + 2),
		spec.Slot(spectestingutils.TestingDutySlot + 3),
	}

	delays := []time.Duration{
		5 * time.Millisecond,
		8000 * time.Millisecond,
		16000 * time.Millisecond,
		24000 * time.Millisecond,
	}
	consensusDataList, proposalDataList, prepareDataList, commitDataList, roundChangeDataList, err := messageDataForSlots(role, pk, slots...)
	if err != nil {
		panic(err)
	}

	return &IntegrationTest{
		Name:             "regular",
		OperatorIDs:      []spectypes.OperatorID{1, 2, 3, 4},
		InitialInstances: nil,
		Duties:           createDutyMap(role, pk, slots, delays),
		// TODO: just check state for 3rd duty
		ExpectedInstances: map[spectypes.OperatorID][]*protocolstorage.StoredInstance{
			1: {
				&protocolstorage.StoredInstance{
					State: &specqbft.State{
						Share:             testingShare(spectestingutils.Testing4SharesSet(), 1),
						ID:                identifier[:],
						Round:             specqbft.FirstRound,
						Height:            2,
						LastPreparedRound: specqbft.FirstRound,
						LastPreparedValue: consensusDataList[3],
						ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
							MsgType:    specqbft.ProposalMsgType,
							Height:     specqbft.FirstHeight,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       proposalDataList[3],
						}),
						Decided:      true,
						DecidedValue: consensusDataList[3],
						ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
									MsgType:    specqbft.ProposalMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       proposalDataList[3],
								}),
							},
						}},
						PrepareContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[3],
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[3],
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[3],
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[4], 4, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[3],
								}),
							},
						}},
						// NOTE: need to keep round keys and commit message, there's a special check for CommitContainer
						CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     2,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitDataList[3],
									},
								},
							},
						}},
						RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							// 2: {
							// 	spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
							// 		MsgType:    specqbft.RoundChangeMsgType,
							// 		Height:     specqbft.FirstHeight,
							// 		Round:      2,
							// 		Identifier: identifier[:],
							// 		Data:       roundChangeDataList[3],
							// 	}),
							// },
							// 3: {
							// 	spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
							// 		MsgType:    specqbft.RoundChangeMsgType,
							// 		Height:     specqbft.FirstHeight,
							// 		Round:      3,
							// 		Identifier: identifier[:],
							// 		Data:       roundChangeDataList[3],
							// 	}),
							// },
						}},
					},
					DecidedMessage: &specqbft.SignedMessage{
						Message: &specqbft.Message{
							MsgType:    specqbft.CommitMsgType,
							Height:     specqbft.FirstHeight,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       spectestingutils.PrepareDataBytes(consensusDataList[3]),
						},
					},
				},
			},
			2: {
				&protocolstorage.StoredInstance{
					State: &specqbft.State{
						Share:                           testingShare(spectestingutils.Testing4SharesSet(), 2),
						ID:                              identifier[:],
						Round:                           specqbft.FirstRound,
						Height:                          specqbft.FirstHeight,
						LastPreparedRound:               0,
						LastPreparedValue:               nil,
						ProposalAcceptedForCurrentRound: nil,
						Decided:                         true,
						DecidedValue:                    consensusDataList[0],
						ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
									MsgType:    specqbft.ProposalMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       proposalDataList[0],
								}),
							},
						}},
						PrepareContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[0],
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[0],
								}),
							},
						}},
						// NOTE: need to keep round keys and commit message data, there's a special check for CommitContainer
						CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										Data: commitDataList[0],
									},
								},
							},
						}},
						RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							2: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.RoundChangeMsgType,
									Height:     specqbft.FirstHeight,
									Round:      2,
									Identifier: identifier[:],
									Data:       roundChangeDataList[0],
								}),
							},
							3: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.RoundChangeMsgType,
									Height:     specqbft.FirstHeight,
									Round:      3,
									Identifier: identifier[:],
									Data:       roundChangeDataList[0],
								}),
							},
						}},
					},
					DecidedMessage: &specqbft.SignedMessage{
						Message: &specqbft.Message{
							MsgType:    specqbft.CommitMsgType,
							Height:     specqbft.FirstHeight,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       spectestingutils.PrepareDataBytes(consensusDataList[0]),
						},
					},
				},
			},
			3: {
				&protocolstorage.StoredInstance{
					State: &specqbft.State{
						Share:             testingShare(spectestingutils.Testing4SharesSet(), 3),
						ID:                identifier[:],
						Round:             specqbft.FirstRound,
						Height:            specqbft.FirstHeight,
						LastPreparedRound: specqbft.FirstRound,
						LastPreparedValue: consensusDataList[0],
						ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
							MsgType:    specqbft.ProposalMsgType,
							Height:     specqbft.FirstHeight,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       proposalDataList[0],
						}),
						Decided:      true,
						DecidedValue: consensusDataList[0],
						ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
									MsgType:    specqbft.ProposalMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       proposalDataList[0],
								}),
							},
						}},
						PrepareContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[0],
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[0],
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[0],
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[4], 4, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[0],
								}),
							},
						}},
						// NOTE: need to keep round keys and commit message data, there's a special check for CommitContainer
						CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										Data: commitDataList[0],
									},
								},
							},
						}},
						RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
					},
					DecidedMessage: &specqbft.SignedMessage{
						Message: &specqbft.Message{
							MsgType:    specqbft.CommitMsgType,
							Height:     specqbft.FirstHeight,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       spectestingutils.PrepareDataBytes(consensusDataList[0]),
						},
					},
				},
			},
			4: {
				&protocolstorage.StoredInstance{
					State: &specqbft.State{
						Share:             testingShare(spectestingutils.Testing4SharesSet(), 4),
						ID:                identifier[:],
						Round:             specqbft.FirstRound,
						Height:            specqbft.FirstHeight,
						LastPreparedRound: specqbft.FirstRound,
						LastPreparedValue: consensusDataList[0],
						ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
							MsgType:    specqbft.ProposalMsgType,
							Height:     specqbft.FirstHeight,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       proposalDataList[0],
						}),
						Decided:      true,
						DecidedValue: consensusDataList[0],
						ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
									MsgType:    specqbft.ProposalMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       proposalDataList[0],
								}),
							},
						}},
						PrepareContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[0],
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[0],
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[0],
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[4], 4, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[0],
								}),
							},
						}},
						// NOTE: need to keep round keys and commit message data, there's a special check for CommitContainer
						CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										Data: commitDataList[0],
									},
								},
							},
						}},
						RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
					},
					DecidedMessage: &specqbft.SignedMessage{
						Message: &specqbft.Message{
							MsgType:    specqbft.CommitMsgType,
							Height:     specqbft.FirstHeight,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       spectestingutils.PrepareDataBytes(consensusDataList[0]),
						},
					},
				},
			},
		},
		StartDutyErrors: map[spectypes.OperatorID]error{
			1: nil,
			2: nil,
			3: nil,
			4: nil,
		},
	}
}

func createDutyMap(role spectypes.BeaconRole, pk []byte, slots []spec.Slot, delays []time.Duration) map[spectypes.OperatorID][]ScheduledDuty {
	return map[spectypes.OperatorID][]ScheduledDuty{
		1: {createScheduledDuty(pk, slots[0], 1, role, delays[0]), createScheduledDuty(pk, slots[1], 1, role, delays[1]), createScheduledDuty(pk, slots[2], 1, role, delays[2]), createScheduledDuty(pk, slots[3], 1, role, delays[3])},
		2: {createScheduledDuty(pk, slots[0], 1, role, delays[0]), createScheduledDuty(pk, slots[1], 1, role, delays[1]), createScheduledDuty(pk, slots[2], 1, role, delays[2]), createScheduledDuty(pk, slots[3], 1, role, delays[3])},
		3: {createScheduledDuty(pk, slots[0], 1, role, delays[0]), createScheduledDuty(pk, slots[2], 1, role, delays[2]), createScheduledDuty(pk, slots[3], 1, role, delays[3])},
		4: {createScheduledDuty(pk, slots[0], 1, role, delays[0]), createScheduledDuty(pk, slots[2], 1, role, delays[2]), createScheduledDuty(pk, slots[3], 1, role, delays[3])},
	}
}

func messageDataForSlots(role spectypes.BeaconRole, pk []byte, slots ...spec.Slot) (consensusDataList, proposalDataList, prepareDataList, commitDataList, roundChangeDataList [][]byte, err error) {
	for _, slot := range slots {
		consensusData, proposalData, prepareData, commitData, roundChangeData, err := messageDataForSlot(role, pk, slot)
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}

		consensusDataList = append(consensusDataList, consensusData)
		proposalDataList = append(proposalDataList, proposalData)
		prepareDataList = append(prepareDataList, prepareData)
		commitDataList = append(commitDataList, commitData)
		roundChangeDataList = append(roundChangeDataList, roundChangeData)
	}

	return consensusDataList, proposalDataList, prepareDataList, commitDataList, roundChangeDataList, nil
}

func messageDataForSlot(role spectypes.BeaconRole, pk []byte, slot spec.Slot) (consensusData, proposalData, prepareData, commitData, roundChangeData []byte, err error) {
	data := &spectypes.ConsensusData{
		Duty:                      createDuty(pk, slot, 1, role),
		AttestationData:           spectestingutils.TestingAttestationData,
		BlockData:                 nil,
		AggregateAndProof:         nil,
		SyncCommitteeBlockRoot:    spec.Root{},
		SyncCommitteeContribution: map[spec.BLSSignature]*altair.SyncCommitteeContribution{},
	}

	data.AttestationData.Slot = slot

	consensusData, err = data.Encode()
	if err != nil {
		return
	}

	proposalData, err = (&specqbft.ProposalData{
		Data:                     consensusData,
		RoundChangeJustification: nil,
		PrepareJustification:     nil,
	}).Encode()
	if err != nil {
		return
	}

	prepareData, err = (&specqbft.PrepareData{
		Data: consensusData,
	}).Encode()
	if err != nil {
		return
	}

	commitData, err = (&specqbft.CommitData{
		Data: consensusData,
	}).Encode()
	if err != nil {
		return
	}

	roundChangeData, err = (&specqbft.RoundChangeData{
		PreparedRound:            0,
		PreparedValue:            nil,
		RoundChangeJustification: nil,
	}).Encode()
	if err != nil {
		return
	}

	return consensusData, proposalData, prepareData, commitData, roundChangeData, nil
}
