package scenarios

// Regular integration test. // TODO: fix
// TODO: consider accepting scenario context - initialize if not passed - for scenario with multiple nodes on same network
// func Regular(role spectypes.BeaconRole) *IntegrationTest {
// 	identifier := spectypes.NewMsgID(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), role)
//
// 	consensusData := &spectypes.ConsensusData{
// 		Duty:                      createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, spectypes.BNRoleAttester)[0],
// 		AttestationData:           spectestingutils.TestingAttestationData,
// 		BlockData:                 nil,
// 		AggregateAndProof:         nil,
// 		SyncCommitteeBlockRoot:    spec.Root{},
// 		SyncCommitteeContribution: map[spec.BLSSignature]*altair.SyncCommitteeContribution{},
// 	}
//
// 	return &IntegrationTest{
// 		Name:             "regular",
// 		OperatorIDs:      []spectypes.OperatorID{1, 2, 3, 4},
// 		InitialInstances: nil,
// 		Duties: map[spectypes.OperatorID][]*spectypes.Duty{
// 			1: createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, role),
// 			2: createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, role),
// 			3: createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, role),
// 			4: createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, role),
// 		},
// 		ExpectedInstances: map[spectypes.OperatorID][]*protocolstorage.StoredInstance{
// 			1: {
// 				storedInstanceForOperatorID(1, identifier, consensusData),
// 			},
// 			2: {
// 				storedInstanceForOperatorID(2, identifier, consensusData),
// 			},
// 			3: {
// 				storedInstanceForOperatorID(3, identifier, consensusData),
// 			},
// 			4: {
// 				storedInstanceForOperatorID(4, identifier, consensusData),
// 			},
// 		},
// 		StartDutyErrors: map[spectypes.OperatorID]error{
// 			1: nil,
// 			2: nil,
// 			3: nil,
// 			4: nil,
// 		},
// 	}
// }
//
// func storedInstanceForOperatorID(operatorID spectypes.OperatorID, identifier spectypes.MessageID, data *spectypes.ConsensusData) *protocolstorage.StoredInstance {
// 	consensusData, err := data.Encode()
// 	if err != nil {
// 		panic(err)
// 	}
//
// 	proposalData, err := (&specqbft.ProposalData{
// 		Data:                     consensusData,
// 		RoundChangeJustification: nil,
// 		PrepareJustification:     nil,
// 	}).Encode()
// 	if err != nil {
// 		panic(err)
// 	}
//
// 	prepareData, err := (&specqbft.PrepareData{
// 		Data: consensusData,
// 	}).Encode()
// 	if err != nil {
// 		panic(err)
// 	}
//
// 	commitData, err := (&specqbft.CommitData{
// 		Data: consensusData,
// 	}).Encode()
// 	if err != nil {
// 		panic(err)
// 	}
//
// 	return &protocolstorage.StoredInstance{
// 		State: &specqbft.State{
// 			Share:             testingShare(spectestingutils.Testing4SharesSet(), operatorID),
// 			ID:                identifier[:],
// 			Round:             specqbft.FirstRound,
// 			Height:            specqbft.FirstHeight,
// 			LastPreparedRound: specqbft.FirstRound,
// 			LastPreparedValue: consensusData,
// 			ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
// 				MsgType:    specqbft.ProposalMsgType,
// 				Height:     specqbft.FirstHeight,
// 				Round:      specqbft.FirstRound,
// 				Identifier: identifier[:],
// 				Data:       proposalData,
// 			}),
// 			Decided:      true,
// 			DecidedValue: consensusData,
// 			ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
// 				specqbft.FirstRound: {
// 					spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
// 						MsgType:    specqbft.ProposalMsgType,
// 						Height:     specqbft.FirstHeight,
// 						Round:      specqbft.FirstRound,
// 						Identifier: identifier[:],
// 						Data:       proposalData,
// 					}),
// 				},
// 			}},
// 			PrepareContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
// 				specqbft.FirstRound: {
// 					spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
// 						MsgType:    specqbft.PrepareMsgType,
// 						Height:     specqbft.FirstHeight,
// 						Round:      specqbft.FirstRound,
// 						Identifier: identifier[:],
// 						Data:       prepareData,
// 					}),
// 					spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
// 						MsgType:    specqbft.PrepareMsgType,
// 						Height:     specqbft.FirstHeight,
// 						Round:      specqbft.FirstRound,
// 						Identifier: identifier[:],
// 						Data:       prepareData,
// 					}),
// 					spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
// 						MsgType:    specqbft.PrepareMsgType,
// 						Height:     specqbft.FirstHeight,
// 						Round:      specqbft.FirstRound,
// 						Identifier: identifier[:],
// 						Data:       prepareData,
// 					}),
// 					spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[4], 4, &specqbft.Message{
// 						MsgType:    specqbft.PrepareMsgType,
// 						Height:     specqbft.FirstHeight,
// 						Round:      specqbft.FirstRound,
// 						Identifier: identifier[:],
// 						Data:       prepareData,
// 					}),
// 				},
// 			}},
// 			// NOTE: need to keep round keys and commit message data, there's a special check for CommitContainer
// 			CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
// 				specqbft.FirstRound: {
// 					&specqbft.SignedMessage{
// 						Message: &specqbft.Message{
// 							Data: commitData,
// 						},
// 					},
// 				},
// 			}},
// 			RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
// 		},
// 		DecidedMessage: &specqbft.SignedMessage{
// 			Message: &specqbft.Message{
// 				MsgType:    specqbft.CommitMsgType,
// 				Height:     specqbft.FirstHeight,
// 				Round:      specqbft.FirstRound,
// 				Identifier: identifier[:],
// 				Data:       spectestingutils.PrepareDataBytes(consensusData),
// 			},
// 		},
// 	}
// }
