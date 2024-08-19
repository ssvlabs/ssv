package logs_catcher

const (
	SlashingMode        = "Slashing"
	BlsVerificationMode = "BlsVerification"
	Slashable           = "slashable"
	NonSlashable        = "nonSlashable"

	//Log conditions:
	EndOfEpochLog                       = "End epoch finished"
	SetUpValidatorLog                   = "set up validator"
	SlashableMessage                    = "\"attester_slashable\":true"
	NonSlashableMessage                 = "\"attester_slashable\":false"
	SlashableAttestationLog             = "slashable attestation"
	SuccessfullySubmittedAttestationLog = "successfully submitted attestation"
	ReconstructSignatureErr             = "could not reconstruct post consensus signature: could not reconstruct beacon sig: failed to verify reconstruct signature: could not reconstruct a valid signature"
	PastRoundErr                        = "failed processing consensus message: could not process msg: invalid signed message: past round"
	ReconstructSignaturesSuccess        = "reconstructed partial signatures"
	SubmittedAttSuccess                 = "✅ successfully submitted attestation"
	GotDutiesSuccess                    = "🗂 got duties"

	MsgHeightField        = "\"msg_height\":%d"
	MsgRoundField         = "\"msg_round\":%d"
	MsgTypeField          = "\"msg_type\":\"%s\""
	ConsensusMsgTypeField = "\"consensus_msg_type\":%d"
	SignersField          = "\"signers\":[%d]"
	ErrorField            = "\"error\":\"%s\""
	DutyIDField           = "\"duty_id\":\"%s\""
	RoleField             = "\"role\":\"%s\""
	SlotField             = "\"slot\":%d"
)

const BeaconProxyContainer = "beacon_proxy"

var SSVNodesContainers = []string{"ssv-node-1", "ssv-node-2", "ssv-node-3", "ssv-node-4"}
