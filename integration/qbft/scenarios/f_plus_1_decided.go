package scenarios

import (
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"

	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
)

func FPlus1Decided(role spectypes.BeaconRole) *IntegrationTest {
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
	_ = roundChangeDataList // TODO: fix
	if err != nil {
		panic(err)
	}

	// 3 validators should start immediately, 4th should have delay between 1st and 2nd duty; 4th can have delay delays[1] / 2
	return &IntegrationTest{
		Name:        "regular",
		OperatorIDs: []spectypes.OperatorID{1, 2, 3, 4},
		ValidatorDelays: map[spectypes.OperatorID]time.Duration{
			1: delays[0],
			2: delays[0],
			3: delays[0],
			4: delays[1],
		},
		InitialInstances: nil,
		Duties:           createFPlus1Duties(role, pk, slots, delays),
		ExpectedInstances: map[spectypes.OperatorID][]*protocolstorage.StoredInstance{
			1: {
				&protocolstorage.StoredInstance{
					State: &specqbft.State{
						Share:             testingShare(spectestingutils.Testing4SharesSet(), 1),
						ID:                identifier[:],
						Round:             specqbft.FirstRound,
						Height:            1,
						LastPreparedRound: specqbft.FirstRound,
						LastPreparedValue: consensusDataList[1],
						ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
							MsgType:    specqbft.ProposalMsgType,
							Height:     1,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       proposalDataList[1],
						}),
						Decided:      true,
						DecidedValue: consensusDataList[1],
						ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.ProposalMsgType,
									Height:     1,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       proposalDataList[1],
								}),
							},
						}},
						PrepareContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     1,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[1],
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     1,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[1],
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     1,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[1],
								}),
							},
						}},
						CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     1,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitDataList[1],
									},
								},
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     1,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitDataList[1],
									},
								},
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     1,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitDataList[1],
									},
								},
							},
						}},
						RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
					},
					DecidedMessage: &specqbft.SignedMessage{
						Message: &specqbft.Message{
							MsgType:    specqbft.CommitMsgType,
							Height:     1,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       spectestingutils.PrepareDataBytes(consensusDataList[1]),
						},
					},
				},
			},
			2: {
				&protocolstorage.StoredInstance{
					State: &specqbft.State{
						Share:             testingShare(spectestingutils.Testing4SharesSet(), 2),
						ID:                identifier[:],
						Round:             specqbft.FirstRound,
						Height:            1,
						LastPreparedRound: specqbft.FirstRound,
						LastPreparedValue: consensusDataList[1],
						ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
							MsgType:    specqbft.ProposalMsgType,
							Height:     1,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       proposalDataList[1],
						}),
						Decided:      true,
						DecidedValue: consensusDataList[1],
						ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.ProposalMsgType,
									Height:     1,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       proposalDataList[1],
								}),
							},
						}},
						PrepareContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     1,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[1],
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     1,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[1],
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     1,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[1],
								}),
							},
						}},
						CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     1,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitDataList[1],
									},
								},
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     1,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitDataList[1],
									},
								},
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     1,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitDataList[1],
									},
								},
							},
						}},
						RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
					},
					DecidedMessage: &specqbft.SignedMessage{
						Message: &specqbft.Message{
							MsgType:    specqbft.CommitMsgType,
							Height:     1,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       spectestingutils.PrepareDataBytes(consensusDataList[1]),
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
						Height:            1,
						LastPreparedRound: specqbft.FirstRound,
						LastPreparedValue: consensusDataList[1],
						ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
							MsgType:    specqbft.ProposalMsgType,
							Height:     1,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       proposalDataList[1],
						}),
						Decided:      true,
						DecidedValue: consensusDataList[1],
						ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.ProposalMsgType,
									Height:     1,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       proposalDataList[1],
								}),
							},
						}},
						PrepareContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     1,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[1],
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     1,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[1],
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     1,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareDataList[1],
								}),
							},
						}},
						CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     1,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitDataList[1],
									},
								},
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     1,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitDataList[1],
									},
								},
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     1,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitDataList[1],
									},
								},
							},
						}},
						RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
					},
					DecidedMessage: &specqbft.SignedMessage{
						Message: &specqbft.Message{
							MsgType:    specqbft.CommitMsgType,
							Height:     1,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       spectestingutils.PrepareDataBytes(consensusDataList[1]),
						},
					},
				},
			},
			4: {
				&protocolstorage.StoredInstance{
					State: &specqbft.State{
						Share:                           testingShare(spectestingutils.Testing4SharesSet(), 4),
						ID:                              identifier[:],
						Round:                           specqbft.FirstRound,
						Height:                          1,
						LastPreparedRound:               0,
						LastPreparedValue:               nil,
						ProposalAcceptedForCurrentRound: nil,
						Decided:                         true,
						DecidedValue:                    consensusDataList[1],
						ProposeContainer:                &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
						PrepareContainer:                &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
						CommitContainer:                 &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
						RoundChangeContainer:            &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
					},
					DecidedMessage: &specqbft.SignedMessage{
						Message: &specqbft.Message{
							MsgType:    specqbft.CommitMsgType,
							Height:     1,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       spectestingutils.PrepareDataBytes(consensusDataList[1]),
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

func createFPlus1Duties(role spectypes.BeaconRole, pk []byte, slots []spec.Slot, delays []time.Duration) map[spectypes.OperatorID][]ScheduledDuty {
	return map[spectypes.OperatorID][]ScheduledDuty{
		1: {createScheduledDuty(pk, slots[0], 1, role, delays[0]), createScheduledDuty(pk, slots[1], 1, role, delays[1])},
		2: {createScheduledDuty(pk, slots[0], 1, role, delays[0]), createScheduledDuty(pk, slots[1], 1, role, delays[1])},
		3: {createScheduledDuty(pk, slots[0], 1, role, delays[0]), createScheduledDuty(pk, slots[1], 1, role, delays[1])},
		4: {},
	}
}
