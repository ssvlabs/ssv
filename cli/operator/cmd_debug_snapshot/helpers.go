package cmd_debug_snapshot

import (
	"fmt"
	"github.com/ilyakaznacheev/cleanenv"
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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

func getLogFilesAbsPaths(fpath string) ([]string, error) {
	logFileName := getFileNameWithoutExt(fpath)
	ext := filepath.Ext(fpath)
	absDirPath, err := filepath.Abs(filepath.Dir(fpath))
	if err != nil {
		return nil, err
	}
	files, err := os.ReadDir(filepath.Dir(fpath))
	if err != nil {
		return nil, err
	}

	matchPattern := logFileName + "*" + ext

	var logFiles []string
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileName := file.Name()

		matches, err := filepath.Match(matchPattern, fileName)
		if err != nil {
			return nil, err
		}
		if matches {
			logFiles = append(logFiles, filepath.Join(absDirPath, fileName))
		}
	}

	return logFiles, nil
}

func sortFiles(paths []string, cfgLogFilePath string) {
	logFilePathPrefix := cfgLogFilePath + "-"
	logFilePathExt := filepath.Ext(cfgLogFilePath)
	
	sort.Slice(paths, func(i, j int) bool {
		if paths[i] == cfgLogFilePath {
			return false
		}
		if paths[j] == cfgLogFilePath {
			return true
		}

		// Extract the timestamps from the filenames
		timestampI := strings.TrimSuffix(strings.TrimPrefix(paths[i], logFilePathPrefix), logFilePathExt)
		timestampJ := strings.TrimSuffix(strings.TrimPrefix(paths[j], logFilePathPrefix), logFilePathExt)

		// Parse the timestamps
		timeI, errI := time.Parse("2006-01-02T15-04-05.000", timestampI)
		timeJ, errJ := time.Parse("2006-01-02T15-04-05.000", timestampJ)

		// If there was an error parsing the timestamps, sort lexicographically
		if errI != nil || errJ != nil {
			return paths[i] < paths[j]
		}

		// Otherwise, sort by timestamp
		return timeI.Before(timeJ)
	})
}
