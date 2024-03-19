package cmd_compress_logs

import (
	"compress/gzip"
	"path/filepath"

	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkCompressLogs(b *testing.B) {
	testCases := []struct {
		SizeInMB int
	}{
		{10},
		{500},
		{1024},
	}

	for _, tc := range testCases {
		b.Run(fmt.Sprintf("%dMB", tc.SizeInMB), func(b *testing.B) {
			testLogFilePath := fmt.Sprintf("test_%dMB.log", tc.SizeInMB)

			gzPath := testLogFilePath + compressedFileExtension

			// generate log file
			err := generateLogFile(tc.SizeInMB, testLogFilePath)
			require.NoError(b, err)

			// compress logs file
			_, err = compressFile(testLogFilePath)
			require.NoError(b, err)

			// get the size of the compressed file
			info, err := os.Stat(gzPath)
			if err != nil {
				b.Fatal(err)
			}
			size := info.Size()

			// check size
			require.Lessf(b, float64(size)/float64(tc.SizeInMB*1024*1024), 0.2, "compressed file size is too large")

			// unzip and compare the files
			unzippedFileName := testLogFilePath + ".unzipped.log"
			err = unzip(testLogFilePath+compressedFileExtension, unzippedFileName)
			require.NoError(b, err)

			eq, err := compareFiles(testLogFilePath, unzippedFileName)
			require.NoError(b, err)
			require.True(b, eq, "files are not equal!")

			// delete files
			if err := deleteFiles(testLogFilePath, gzPath, unzippedFileName); err != nil {
				b.Fatal(err)
			}
		})
	}
}

func compareFiles(file1Path, file2Path string) (bool, error) {
	file1, err := os.Open(filepath.Clean(file1Path))
	if err != nil {
		return false, err
	}
	defer func() {
		_ = file1.Close()
	}()
	file2, err := os.Open(filepath.Clean(file2Path))
	if err != nil {
		return false, err
	}
	defer file2.Close()

	scanner1 := bufio.NewScanner(file1)
	scanner2 := bufio.NewScanner(file2)

	for scanner1.Scan() && scanner2.Scan() {
		if scanner1.Text() != scanner2.Text() {
			return false, nil
		}
	}

	if err := scanner1.Err(); err != nil {
		return false, err
	}
	if err := scanner2.Err(); err != nil {
		return false, err
	}

	// check both were completely read and have the same size
	if scanner1.Scan() != scanner2.Scan() {
		return false, nil
	}

	return true, nil
}

// used to write to the file in chunks to avoid memory issues with large files
const chunkSize = 200 * 1024 * 1024 // 200MB

// generateLogFile generates a valid log file with the given size in MB
// chunks are used to write the file to avoid memory issues with large files
func generateLogFile(sizeInMB int, path string) error {
	file, err := os.Create(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer file.Close()

	var buffer bytes.Buffer
	var totalSize int64 = 0
	var i int64 = 0

	for totalSize < int64(sizeInMB)*1024*1024 {
		// some random dummy log entry
		entry := fmt.Sprintf("{\"L\":\"DEBUG\",\"T\":\"\",\"N\":\"P2PNetwork\",\"M\":\"subscribing to subnets\",\"subnets\":\"%032d\"}\n", i)
		buffer.WriteString(entry)

		if buffer.Len() >= chunkSize {
			// Write buffer to file and clear buffer
			if _, err := file.WriteString(buffer.String()); err != nil {
				return err
			}
			buffer.Reset()
		}

		totalSize += int64(len(entry))
		i++
	}

	// Write remaining buffer to file
	if buffer.Len() > 0 {
		if _, err := file.WriteString(buffer.String()); err != nil {
			return err
		}
	}

	return nil
}

func unzip(src string, dest string) error {
	// Open the gzip file
	r, err := os.Open(filepath.Clean(src))
	if err != nil {
		return err
	}
	defer func() {
		_ = r.Close()
	}()

	// Create a gzip reader
	gr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer func() {
		_ = gr.Close()
	}()

	// Create destination file
	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	// Copy the gzip reader to the destination file
	_, err = io.Copy(destFile, gr)
	if err != nil {
		return err
	}

	return nil
}

func deleteFiles(paths ...string) error {
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

//func cleanPath(path string) (string, error) {
//	absPath, err := filepath.Abs(filepath.Clean(path))
//
//	if err != nil {
//		return path, err
//	}
//
//	return absPath, nil
//}
