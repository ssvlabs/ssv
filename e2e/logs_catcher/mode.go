package logs_catcher

type Mode = string
type SubMode = string

const (
	DutiesMode          = "Duties"
	BlsVerificationMode = "BlsVerification"
	// Add other modes as necessary
)

const (
	Slashable       = "Slashable"
	NonSlashable    = "NonSlashable"
	RsaVerification = "RsaVerification"
)
