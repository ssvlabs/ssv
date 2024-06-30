package logs_catcher

const (
	SlashingMode        = "Slashing"
	BlsVerificationMode = "BlsVerification"
	Slashable           = "slashable"
	NonSlashable        = "nonSlashable"

	//Log conditions:
	endOfEpochLog                       = "End epoch finished"
	setUpValidatorLog                   = "set up validator"
	slashableMessage                    = "\"attester_slashable\":true"
	nonSlashableMessage                 = "\"attester_slashable\":false"
	slashableAttestationLog             = "slashable attestation"
	successfullySubmittedAttestationLog = "successfully submitted attestation"
	failedVerifySigLog                  = "failed processing consensus message: could not process msg: invalid signed message: msg signature invalid: failed to verify signature"
	reconstructSignatureErr             = "could not reconstruct post consensus signature: could not reconstruct beacon sig: failed to verify reconstruct signature: could not reconstruct a valid signature"
	pastRoundErr                        = "failed processing consensus message: could not process msg: invalid signed message: past round"
	reconstructSignaturesSuccess        = "reconstructed partial signatures"
	submittedAttSuccess                 = "✅ successfully submitted attestation"
	gotDutiesSuccess                    = "🗂 got duties"

	msgHeightField        = "\"msg_height\":%d"
	msgRoundField         = "\"msg_round\":%d"
	msgTypeField          = "\"msg_type\":\"%s\""
	consensusMsgTypeField = "\"consensus_msg_type\":%d"
	signersField          = "\"signers\":[%d]"
	errorField            = "\"error\":\"%s\""
	dutyIDField           = "\"duty_id\":\"%s\""
	roleField             = "\"role\":\"%s\""
	slotField             = "\"slot\":%d"
)

const beaconProxyContainer = "beacon_proxy"

var ssvNodesContainers = []string{"ssv-node-1", "ssv-node-2", "ssv-node-3", "ssv-node-4"}
