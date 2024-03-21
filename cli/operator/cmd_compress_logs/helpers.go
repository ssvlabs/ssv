package cmd_compress_logs

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
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
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}

	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func createFileCopy(file string, destDir string) error {
	srcFile, err := os.Open(file)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	destFile, err := os.Create(filepath.Join(destDir, filepath.Base(srcFile.Name())))
	if err != nil {
		return err
	}

	defer destFile.Close()

	// Copy the contents of the source file to the destination file
	_, err = io.Copy(destFile, srcFile)
	if err != nil {
		return err
	}

	return nil
}

func copyFilesToDir(destDir string, files []string) error {
	for _, file := range files {
		if err := createFileCopy(file, destDir); err != nil {
			return err
		}
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

func compressDirectory(srcDirPath string) (string, error) {
	dirName := filepath.Base(srcDirPath)
	parentDir := filepath.Dir(srcDirPath)
	newSrcDir := filepath.Join(parentDir, dirName) // this prevents having all nested directories in the tarball

	tarFile, err := os.Create(dirName + compressedFileExtension)
	if err != nil {
		return "", fmt.Errorf("can't create a tar file with name %s: %w", dirName, err)
	}
	defer tarFile.Close()

	gzWriter := gzip.NewWriter(tarFile)
	defer gzWriter.Close()

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

func untarGzFile(gzFilePath string) (string, error) {
	gzFile, err := os.Open(gzFilePath + compressedFileExtension)
	if err != nil {
		return "", err
	}
	defer gzFile.Close()

	gzReader, err := gzip.NewReader(gzFile)
	if err != nil {
		return "", err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	var firstDirName string

	outputDir := filepath.Join(filepath.Dir(gzFilePath), "unsizpped")

	for {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			return "", err
		}

		target := filepath.Join(filepath.Dir(outputDir), header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return "", err
			}

			if firstDirName == "" {
				firstDirName = header.Name
			}

		case tar.TypeReg:
			file, err := os.Create(target)
			if err != nil {
				return "", err
			}
			defer file.Close()

			if _, err := io.Copy(file, tarReader); err != nil {
				return "", err
			}
		}
	}

	return firstDirName, nil
}
