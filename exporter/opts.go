package exporter

type Options struct {
	Enabled     bool   `yaml:"Enabled" env:"EXPORTER" env-default:"false" env-description:"Enable exporter mode to track post-consensus participations"`
	Mode        string `yaml:"Mode" env:"EXPORTER_MODE" env-default:"standard" env-description:"Set to 'archive' to also track pre-consensus and consensus steps. Defaults to 'standard'"`
	RetainSlots uint64 `yaml:"RetainSlots" env:"EXPORTER_RETAIN_SLOTS" env-default:"50400" env-description:"Number of slots to retain in export data"`

	// AuditorEnabled enables the in-node trace<->schedule auditor (default off).
	// The auditor never filters traces; it only detects and explains mismatches.
	AuditorEnabled bool `yaml:"AuditorEnabled" env:"EXPORTER_AUDITOR" env-default:"false" env-description:"Enable exporter auditor to detect trace<->schedule mismatches and persist findings"`
	// AuditorRPCFallback enables beacon RPC checks for mismatching indices (recommended on).
	AuditorRPCFallback bool `yaml:"AuditorRPCFallback" env:"EXPORTER_AUDITOR_RPC_FALLBACK" env-default:"true" env-description:"Enable auditor beacon RPC fallback to confirm duties when local duty store is incomplete"`
	// AuditorDelaySlots controls audit delay: audit runs at (slot - delay). Default 4 (slot+4).
	AuditorDelaySlots uint64 `yaml:"AuditorDelaySlots" env:"EXPORTER_AUDITOR_DELAY_SLOTS" env-default:"4" env-description:"Delay in slots before auditing a slot (default 4)"`
}

const (
	ModeArchive  = "archive"
	ModeStandard = "standard"
)
