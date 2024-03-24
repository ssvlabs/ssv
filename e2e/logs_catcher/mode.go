package logs_catcher

type Mode = string
type SubMode = string

const (
	RestartMode         = "Restart"
	BlsVerificationMode = "BlsVerification"
	// Add other modes as necessary
)

const (
	Slashable       = "Slashable"
	NonSlashable    = "NonSlashable"
	RsaVerification = "RsaVerification"
)
