package exporter

type ExporterOptions struct {
	Enabled     bool   `yaml:"Enabled" env:"EXPORTER" env-default:"false" env-description:"Enable exporter mode to track post-consensus participations (heavy resource usage)"`
	Mode        string `yaml:"Mode" env:"EXPORTER_MODE" env-default:"" env-description:"Extend exporter to track pre-consensus and consensus steps (heaviest resource usage)"`
	RetainSlots uint64 `yaml:"RetainSlots" env:"EXPORTER_RETAIN_SLOTS" env-default:"50400" env-description:"Number of slots to retain in export data"`
}

const (
	ExporterModeArchive = "archive"
)
