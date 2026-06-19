package exporter

type Options struct {
	Enabled     bool   `yaml:"Enabled" env:"EXPORTER" env-description:"Enable exporter mode to track post-consensus participations"`
	Mode        string `yaml:"Mode" env:"EXPORTER_MODE" env-description:"Set to 'archive' to also track pre-consensus and consensus steps. Defaults to 'standard'"`
	RetainSlots uint64 `yaml:"RetainSlots" env:"EXPORTER_RETAIN_SLOTS" env-description:"Number of slots to retain in export data"`
}

const (
	ModeArchive  = "archive"
	ModeStandard = "standard"
)

func (o *Options) ApplyDefaults() {
	o.Mode = ModeStandard
	o.RetainSlots = 50400
}
