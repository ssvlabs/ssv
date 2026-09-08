// Command list prints every FAULT menu value, one per line, in menu order. It exists so the M3
// runbook's boot-smoke loop (qa/FAULTS.md) enumerates the menu from the code — via faults.Names() —
// rather than from a hardcoded shell list that can drift from the registry in qa/faults/faults.go.
package main

import (
	"fmt"

	"github.com/ssvlabs/ssv/qa/faults"
)

func main() {
	for _, name := range faults.Names() {
		fmt.Println(name)
	}
}
