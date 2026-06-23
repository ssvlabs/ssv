package exporter

type Options struct {
	Enabled      bool   `yaml:"Enabled" env:"EXPORTER" env-default:"false" env-description:"Enable exporter mode to track validator duties and network consensus participation (full duty tracing)"`
	RetainEpochs uint64 `yaml:"RetainEpochs" env:"EXPORTER_RETAIN_EPOCHS" env-default:"0" env-description:"Best-effort retention: prune on-disk duty traces older than this many epochs. 0 (default) retains indefinitely. Enforced forward from process start — history that ages out while the node is down is not reclaimed after a restart"`
}
