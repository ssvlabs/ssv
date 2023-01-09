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

	consensusData, proposalData, prepareData, commitData, _, err := messageDataForSlot(role, pk, slots[3])
	if err != nil {
		panic(err)
	}

	return &IntegrationTest{
		Name:             "round change",
		OperatorIDs:      []spectypes.OperatorID{1, 2, 3, 4},
		InitialInstances: nil,
		Duties: map[spectypes.OperatorID][]scheduledDuty{
			1: {createScheduledDuty(pk, slots[0], 1, role, delays[0]), createScheduledDuty(pk, slots[1], 1, role, delays[1]), createScheduledDuty(pk, slots[2], 1, role, delays[2]), createScheduledDuty(pk, slots[3], 1, role, delays[3])},
			2: {createScheduledDuty(pk, slots[0], 1, role, delays[0]), createScheduledDuty(pk, slots[1], 1, role, delays[1]), createScheduledDuty(pk, slots[2], 1, role, delays[2]), createScheduledDuty(pk, slots[3], 1, role, delays[3])},
			3: {createScheduledDuty(pk, slots[0], 1, role, delays[0]), createScheduledDuty(pk, slots[2], 1, role, delays[2]), createScheduledDuty(pk, slots[3], 1, role, delays[3])},
			4: {createScheduledDuty(pk, slots[0], 1, role, delays[0]), createScheduledDuty(pk, slots[2], 1, role, delays[2]), createScheduledDuty(pk, slots[3], 1, role, delays[3])},
		},
		// TODO: just check state for 3rd duty
		// TODO: consider using a validation function func(map[spectypes.OperatorID][]*protocolstorage.StoredInstance) bool
		ExpectedInstances: map[spectypes.OperatorID][]*protocolstorage.StoredInstance{
			1: {
				&protocolstorage.StoredInstance{
					State: &specqbft.State{
						Share:             testingShare(spectestingutils.Testing4SharesSet(), 1),
						ID:                identifier[:],
						Round:             specqbft.FirstRound,
						Height:            2,
						LastPreparedRound: specqbft.FirstRound,
						LastPreparedValue: consensusData,
						ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
							MsgType:    specqbft.ProposalMsgType,
							Height:     2,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       proposalData,
						}),
						Decided:      true,
						DecidedValue: consensusData,
						ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.ProposalMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       proposalData,
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
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[4], 4, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
							},
						}},
						CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     2,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitData,
									},
								},
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     2,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitData,
									},
								},
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     2,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitData,
									},
								},
							},
						}},
						RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
					},
					DecidedMessage: &specqbft.SignedMessage{
						Message: &specqbft.Message{
							MsgType:    specqbft.CommitMsgType,
							Height:     2,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       spectestingutils.PrepareDataBytes(consensusData),
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
						Height:            2,
						LastPreparedRound: 1,
						LastPreparedValue: consensusData,
						ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
							MsgType:    specqbft.ProposalMsgType,
							Height:     2,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       proposalData,
						}),
						Decided:      true,
						DecidedValue: consensusData,
						ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.ProposalMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       proposalData,
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
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[4], 4, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
							},
						}},
						CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     2,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitData,
									},
								},
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     2,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitData,
									},
								},
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     2,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitData,
									},
								},
							},
						}},
						RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
					},
					DecidedMessage: &specqbft.SignedMessage{
						Message: &specqbft.Message{
							MsgType:    specqbft.CommitMsgType,
							Height:     2,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       spectestingutils.PrepareDataBytes(consensusData),
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
						Height:            2,
						LastPreparedRound: specqbft.FirstRound,
						LastPreparedValue: consensusData,
						ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
							MsgType:    specqbft.ProposalMsgType,
							Height:     2,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       proposalData,
						}),
						Decided:      true,
						DecidedValue: consensusData,
						ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.ProposalMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       proposalData,
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
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[4], 4, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
							},
						}},
						CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     2,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitData,
									},
								},
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     2,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitData,
									},
								},
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     2,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitData,
									},
								},
							},
						}},
						RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
					},
					DecidedMessage: &specqbft.SignedMessage{
						Message: &specqbft.Message{
							MsgType:    specqbft.CommitMsgType,
							Height:     2,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       spectestingutils.PrepareDataBytes(consensusData),
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
						Height:            2,
						LastPreparedRound: specqbft.FirstRound,
						LastPreparedValue: consensusData,
						ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
							MsgType:    specqbft.ProposalMsgType,
							Height:     2,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       proposalData,
						}),
						Decided:      true,
						DecidedValue: consensusData,
						ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.ProposalMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       proposalData,
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
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[4], 4, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     2,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
							},
						}},
						CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     2,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitData,
									},
								},
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     2,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitData,
									},
								},
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										MsgType:    specqbft.CommitMsgType,
										Height:     2,
										Round:      specqbft.FirstRound,
										Identifier: identifier[:],
										Data:       commitData,
									},
								},
							},
						}},
						RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
					},
					DecidedMessage: &specqbft.SignedMessage{
						Message: &specqbft.Message{
							MsgType:    specqbft.CommitMsgType,
							Height:     2,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       spectestingutils.PrepareDataBytes(consensusData),
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
