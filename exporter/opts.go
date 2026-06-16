package exporter

type Options struct {
	Enabled      bool   `yaml:"Enabled" env:"EXPORTER" env-default:"false" env-description:"Enable exporter mode to track validator duties and network consensus participation (full duty tracing)"`
	RetainEpochs uint64 `yaml:"RetainEpochs" env:"EXPORTER_RETAIN_EPOCHS" env-default:"0" env-description:"Number of epochs of duty-trace history to retain; 0 (default) retains indefinitely"`
}
