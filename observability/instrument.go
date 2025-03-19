package observability

import "fmt"

func InstrumentName(namespace, name string) string {
	return fmt.Sprintf("%s.%s", namespace, name)
}
