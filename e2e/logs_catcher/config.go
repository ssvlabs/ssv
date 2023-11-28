package logs_catcher

type Config struct {
	IgnoreContainers []string
	Fatalers         []map[string]string
	Restarters       []map[string]string
}
