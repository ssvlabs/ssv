package exporter

type Options struct {
	Enabled     bool   `yaml:"Enabled" env:"EXPORTER" env-default:"false" env-description:"Enable exporter mode to track post-consensus participations"`
	Mode        string `yaml:"Mode" env:"EXPORTER_MODE" env-default:"standard" env-description:"Set to 'archive' to also track pre-consensus and consensus steps. Defaults to 'standard'"`
	RetainSlots uint64 `yaml:"RetainSlots" env:"EXPORTER_RETAIN_SLOTS" env-default:"50400" env-description:"Number of slots to retain in export data"`
}

const (
	ModeArchive  = "archive"
	ModeStandard = "standard"
)
