package scenarios

import (
	"github.com/attestantio/go-eth2-client/spec/altair"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/herumi/bls-eth-go-binary/bls"

	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
)

// Regular integration test.
// TODO: consider accepting scenario context - initialize if not passed - for scenario with multiple nodes on same network
func Regular() *IntegrationTest {
	roles := []spectypes.BeaconRole{
		spectypes.BNRoleAttester,
	}

	// TODO: use multiple roles
	identifier := spectypes.NewMsgID(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectypes.BNRoleAttester)

	consensusData, err := (&spectypes.ConsensusData{
		Duty:                      createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, spectypes.BNRoleAttester)[0],
		AttestationData:           spectestingutils.TestingAttestationData,
		BlockData:                 nil,
		AggregateAndProof:         nil,
		SyncCommitteeBlockRoot:    spec.Root{},
		SyncCommitteeContribution: map[spec.BLSSignature]*altair.SyncCommitteeContribution{},
	}).Encode()
	if err != nil {
		panic(err)
	}

	proposalData, err := (&specqbft.ProposalData{
		Data:                     consensusData,
		RoundChangeJustification: nil,
		PrepareJustification:     nil,
	}).Encode()
	if err != nil {
		panic(err)
	}

	prepareData, err := (&specqbft.PrepareData{
		Data: consensusData,
	}).Encode()
	if err != nil {
		panic(err)
	}

	commitData, err := (&specqbft.CommitData{
		Data: consensusData,
	}).Encode()
	if err != nil {
		panic(err)
	}

	return &IntegrationTest{
		Name:             "regular",
		OperatorIDs:      []spectypes.OperatorID{1, 2, 3, 4},
		InitialInstances: nil,
		Duties: map[spectypes.OperatorID][]*spectypes.Duty{
			1: createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, roles...),
			2: createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, roles...),
			3: createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, roles...),
			4: createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, roles...),
		},
		ExpectedInstances: map[spectypes.OperatorID][]*protocolstorage.StoredInstance{
			1: {
				{
					State: &specqbft.State{
						Share:             testingShare(spectestingutils.Testing4SharesSet(), 1),
						ID:                identifier[:],
						Round:             specqbft.FirstRound,
						Height:            specqbft.FirstHeight,
						LastPreparedRound: specqbft.FirstRound,
						LastPreparedValue: consensusData,
						ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
							MsgType:    specqbft.ProposalMsgType,
							Height:     specqbft.FirstHeight,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       proposalData,
						}),
						Decided:      true,
						DecidedValue: consensusData,
						ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
									MsgType:    specqbft.ProposalMsgType,
									Height:     specqbft.FirstHeight,
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
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[4], 4, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
							},
						}},
						// NOTE: need to keep round keys and commit message data, there's a special check for CommitContainer
						CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										Data: commitData,
									},
								},
							},
						}},
						RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
					},
					DecidedMessage: spectestingutils.MultiSignQBFTMsg([]*bls.SecretKey{spectestingutils.Testing4SharesSet().Shares[1], spectestingutils.Testing4SharesSet().Shares[2]}, []spectypes.OperatorID{1, 2}, &specqbft.Message{
						MsgType:    specqbft.CommitMsgType,
						Height:     specqbft.FirstHeight,
						Round:      specqbft.FirstRound,
						Identifier: identifier[:],
						Data:       spectestingutils.PrepareDataBytes(consensusData),
					}),
				},
			},
			2: {
				{
					State: &specqbft.State{
						Share:             testingShare(spectestingutils.Testing4SharesSet(), 2),
						ID:                identifier[:],
						Round:             specqbft.FirstRound,
						Height:            specqbft.FirstHeight,
						LastPreparedRound: specqbft.FirstRound,
						LastPreparedValue: consensusData,
						ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
							MsgType:    specqbft.ProposalMsgType,
							Height:     specqbft.FirstHeight,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       proposalData,
						}),
						Decided:      true,
						DecidedValue: consensusData,
						ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
									MsgType:    specqbft.ProposalMsgType,
									Height:     specqbft.FirstHeight,
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
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[4], 4, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
							},
						}},
						// NOTE: need to keep round keys and commit message data, there's a special check for CommitContainer
						CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										Data: commitData,
									},
								},
							},
						}},
						RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
					},
					DecidedMessage: spectestingutils.MultiSignQBFTMsg([]*bls.SecretKey{spectestingutils.Testing4SharesSet().Shares[1], spectestingutils.Testing4SharesSet().Shares[2]}, []spectypes.OperatorID{1, 2}, &specqbft.Message{
						MsgType:    specqbft.CommitMsgType,
						Height:     specqbft.FirstHeight,
						Round:      specqbft.FirstRound,
						Identifier: identifier[:],
						Data:       spectestingutils.PrepareDataBytes(consensusData),
					}),
				},
			},
			3: {
				{
					State: &specqbft.State{
						Share:             testingShare(spectestingutils.Testing4SharesSet(), 3),
						ID:                identifier[:],
						Round:             specqbft.FirstRound,
						Height:            specqbft.FirstHeight,
						LastPreparedRound: specqbft.FirstRound,
						LastPreparedValue: consensusData,
						ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
							MsgType:    specqbft.ProposalMsgType,
							Height:     specqbft.FirstHeight,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       proposalData,
						}),
						Decided:      true,
						DecidedValue: consensusData,
						ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
									MsgType:    specqbft.ProposalMsgType,
									Height:     specqbft.FirstHeight,
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
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[4], 4, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
							},
						}},
						// NOTE: need to keep round keys and commit message data, there's a special check for CommitContainer
						CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										Data: commitData,
									},
								},
							},
						}},
						RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
					},
					DecidedMessage: spectestingutils.MultiSignQBFTMsg([]*bls.SecretKey{spectestingutils.Testing4SharesSet().Shares[1], spectestingutils.Testing4SharesSet().Shares[2]}, []spectypes.OperatorID{1, 2}, &specqbft.Message{
						MsgType:    specqbft.CommitMsgType,
						Height:     specqbft.FirstHeight,
						Round:      specqbft.FirstRound,
						Identifier: identifier[:],
						Data:       spectestingutils.PrepareDataBytes(consensusData),
					}),
				},
			},
			4: {
				{
					State: &specqbft.State{
						Share:             testingShare(spectestingutils.Testing4SharesSet(), 4),
						ID:                identifier[:],
						Round:             specqbft.FirstRound,
						Height:            specqbft.FirstHeight,
						LastPreparedRound: specqbft.FirstRound,
						LastPreparedValue: consensusData,
						ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
							MsgType:    specqbft.ProposalMsgType,
							Height:     specqbft.FirstHeight,
							Round:      specqbft.FirstRound,
							Identifier: identifier[:],
							Data:       proposalData,
						}),
						Decided:      true,
						DecidedValue: consensusData,
						ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
									MsgType:    specqbft.ProposalMsgType,
									Height:     specqbft.FirstHeight,
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
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
								spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[4], 4, &specqbft.Message{
									MsgType:    specqbft.PrepareMsgType,
									Height:     specqbft.FirstHeight,
									Round:      specqbft.FirstRound,
									Identifier: identifier[:],
									Data:       prepareData,
								}),
							},
						}},
						// NOTE: need to keep round keys and commit message data, there's a special check for CommitContainer
						CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
							specqbft.FirstRound: {
								&specqbft.SignedMessage{
									Message: &specqbft.Message{
										Data: commitData,
									},
								},
							},
						}},
						RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
					},
					DecidedMessage: spectestingutils.MultiSignQBFTMsg([]*bls.SecretKey{spectestingutils.Testing4SharesSet().Shares[1], spectestingutils.Testing4SharesSet().Shares[2]}, []spectypes.OperatorID{1, 2}, &specqbft.Message{
						MsgType:    specqbft.CommitMsgType,
						Height:     specqbft.FirstHeight,
						Round:      specqbft.FirstRound,
						Identifier: identifier[:],
						Data:       spectestingutils.PrepareDataBytes(consensusData),
					}),
				},
			},
		},
		ExpectedErrors: nil, // TODO: if it's nil, don't check errors
		OutputMessages: nil,
	}
}
