package cmd_compress_logs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ilyakaznacheev/cleanenv"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
)

func setupGlobal(cfg *config) (*zap.Logger, error) {
	if globalArgs.ConfigPath == "" {
		return nil, fmt.Errorf("config path is required")
	}

	if err := cleanenv.ReadConfig(globalArgs.ConfigPath, cfg); err != nil {
		return nil, fmt.Errorf("could not read config: %w", err)
	}

	return setGlobalLogger(cfg)
}

func setGlobalLogger(cfg *config) (*zap.Logger, error) {
	err := logging.SetGlobalLogger(
		cfg.LogLevel,
		cfg.LogLevelFormat,
		"console",
		nil,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to set global logger: %w", err)
	}

	return zap.L(), nil
}

func getFileNameWithoutExt(path string) string {
	if path == "" {
		return ""
	}
	filenameWithExt := filepath.Base(path) // Get the file name with extension
	extension := filepath.Ext(path)        // Get the file extension

	filename := filenameWithExt[0 : len(filenameWithExt)-len(extension)] // Remove the extension from the filename
	return filename
}

func calcFileSize(path string) (int64, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return 0, err
	}

	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func getLogFilesAbsPaths(path string) ([]string, error) {
	logFileName := getFileNameWithoutExt(path)
	ext := filepath.Ext(path)
	absDirPath, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return nil, err
	}

	var logFiles []string
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileName := file.Name()
		filePrefix := strings.TrimSuffix(fileName, filepath.Ext(fileName))

		// filter to have .log files
		if filepath.Ext(fileName) == ext && strings.Contains(filePrefix, logFileName) {
			logFiles = append(logFiles, filepath.Join(absDirPath, fileName))
		}
	}

	return logFiles, nil
}
