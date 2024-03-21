package cmd_compress_logs

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	globalconfig "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/utils/commons"
)

type config struct {
	globalconfig.GlobalConfig `yaml:"global"`
}

var cfg config
var globalArgs globalconfig.Args

const compressedFileExtension = ".gz"

// CompressLogsCmd is the command to compress logs file with gzip
var CompressLogsCmd = &cobra.Command{
	Use:   "compress-logs",
	Short: "Compresses logs",
	Run: func(cmd *cobra.Command, args []string) {
		logger, err := setupGlobal(&cfg)

		if err != nil {
			logger.Fatal("initialization failed", zap.Error(err))
		}

		logger.Info(fmt.Sprintf("starting logs compression %v", commons.GetBuildData()))

		logsFilePath := cfg.LogFilePath

		fileSizeBytes, err := compressLogFiles(logsFilePath)
		if err != nil {
			logger.Fatal("logs file compression failed", zap.Error(err))
		}

		logger.Info("✅ logs compression finished", zap.Uint64("file_size_bytes", uint64(fileSizeBytes)))
	},
}

func compressLogFiles(logConfigPath string) (int64, error) {
	// Find all log files in the directory of the provided file
	logFiles, err := getLogFilesAbsPaths(logConfigPath)
	if err != nil {
		return 0, err
	}

	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "logs_"+fmt.Sprint(time.Now().Unix()))

	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmpDir) // clean up

	for _, file := range logFiles {
		fmt.Println(file)
	}
	fmt.Println(tmpDir)

	err = copyFilesToDir(tmpDir, logFiles)
	if err != nil {
		return 0, err
	}

	tarAbsPath, err := compressDirectory(tmpDir, logConfigPath+compressedFileExtension)
	if err != nil {
		return 0, err
	}

	compressedFile, err := os.Open(tarAbsPath)
	if err != nil {
		return 0, err
	}
	info, err := compressedFile.Stat()
	if err != nil {
		return 0, err
	}

	return info.Size(), nil
}

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

func init() {
	globalconfig.ProcessConfigArg(&cfg, &globalArgs, CompressLogsCmd)
	globalconfig.ProcessHelpCmd(&cfg, &globalArgs, CompressLogsCmd)
}

func copyFilesToDir(destDir string, files []string) error {
	// Create the destination directory
	err := os.MkdirAll(destDir, 0755)
	if err != nil {
		return err
	}

	for _, file := range files {
		// Open the source file
		srcFile, err := os.Open(file)
		if err != nil {
			return err
		}

		fmt.Println("Xxxxx", filepath.Base(srcFile.Name()))
		// Create the destination file
		destFile, err := os.Create(filepath.Join(destDir, filepath.Base(srcFile.Name())))
		if err != nil {
			_ = srcFile.Close()
			return err
		}

		// Copy the contents of the source file to the destination file
		_, err = io.Copy(destFile, srcFile)
		if err != nil {
			srcFile.Close()
			destFile.Close()
			return err
		}

		_ = srcFile.Close()
		_ = destFile.Close()
	}
	return nil
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

func compressDirectory(srcDir string, tarFileName string) (string, error) {
	dirName := filepath.Base(srcDir)
	parentDir := filepath.Dir(srcDir)
	newSrcDir := filepath.Join(parentDir, dirName)

	tarFile, err := os.Create(tarFileName)
	if err != nil {
		return "", err
	}
	if err != nil {
		return "", err
	}
	p, err := filepath.Abs(tarFile.Name())
	if err != nil {
		return "", err
	}
	fmt.Println("tarFile", p)
	defer tarFile.Close()

	// Create a gzip writer
	gzWriter := gzip.NewWriter(tarFile)
	defer gzWriter.Close()

	// Create a tar writer
	tw := tar.NewWriter(gzWriter)
	defer tw.Close()

	// recursively walk the directory and write the contents to the tarball
	err = filepath.Walk(newSrcDir, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Create a new dir/file header
		header, err := tar.FileInfoHeader(fi, fi.Name())
		if err != nil {
			return err
		}

		// Update the name to correctly reflect the desired destination when un-taring
		header.Name = strings.TrimPrefix(filepath.ToSlash(file), parentDir)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// If it's not a dir, write file content
		if !fi.IsDir() {
			data, err := os.Open(file)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, data); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return "", err
	}

	absTarFilePath, err := filepath.Abs(tarFile.Name())
	if err != nil {
		return "", err
	}

	return absTarFilePath, nil
}

func getFileNameWithoutExt(path string) string {
	filenameWithExt := filepath.Base(path) // Get the file name with extension
	extension := filepath.Ext(path)        // Get the file extension

	filename := filenameWithExt[0 : len(filenameWithExt)-len(extension)] // Remove the extension from the filename
	return filename
}
