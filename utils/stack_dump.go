package utils

import (
	"os"
	"path/filepath"
	"runtime"
)

var DebugDataDir = ""

func FullGoroutineStackDump() error {
	buffer := make([]byte, 128*1024*1024)
	length := runtime.Stack(buffer, true)

	fileName := filepath.Join(DebugDataDir, "full-goroutine-stack-dump.txt")
	return os.WriteFile(fileName, buffer[:length], 0600)
}
