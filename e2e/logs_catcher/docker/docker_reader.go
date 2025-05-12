package docker

import (
	"bufio"
	"context"
	"io"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"

	"github.com/ssvlabs/ssv/e2e/logs_catcher/logs"
)

type Streamer interface {
	ContainerLogs(
		ctx context.Context,
		container string,
		options types.ContainerLogsOptions,
	) (io.ReadCloser, error)
}

func New() (*client.Client, error) {
	return client.NewClientWithOpts(client.FromEnv)
}

func StreamDockerLogs(
	ctx context.Context,
	cli Streamer,
	containerName string,
	logsChan chan string,
) error {
	i, err := cli.ContainerLogs(ctx, containerName, types.ContainerLogsOptions{
		ShowStderr: true,
		ShowStdout: true,
		Timestamps: false,
		Follow:     true,
		//Tail:       "40",
	})
	if err != nil {
		return err
	}
	defer i.Close()
	return processLogs(ctx, i, logsChan)
}

func processLogs(ctx context.Context, reader io.ReadCloser, logChan chan<- string) error {
	scanner := bufio.NewScanner(reader)
	var tempBuffer []byte

	for scanner.Scan() {
		// Check for any scanner errors
		if err := scanner.Err(); err != nil {
			return errors.Wrap(err, "error reading logs")
		}

		line := string(scanner.Bytes())
		// Check if the line is longer than the Docker header
		if len(line) <= 8 {
			continue
		}

		// Docker header is 8 bytes, strip it off
		line = line[8:]

		// Check if the line ends with a newline character

		// If tempBuffer is not empty, append the line to it
		if len(tempBuffer) != 0 {
			line = string(append(tempBuffer, []byte(line)...))
			tempBuffer = nil
		}

		// Send the complete log message to the channel
		select {
		case logChan <- line:
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func DockerLogs(
	ctx context.Context,
	cli Streamer,
	containerName string,
	lastMessage string,
) (logs.RAW, error) {
	i, err := cli.ContainerLogs(ctx, containerName, types.ContainerLogsOptions{
		ShowStderr: true,
		ShowStdout: true,
		Timestamps: false,
		Follow:     false,
		//Tail:       "40",
	})
	if err != nil {
		return nil, err
	}
	defer i.Close()

	logs, err := io.ReadAll(i)
	if err != nil {
		return nil, err
	}

	// Split the logs into lines
	res := strings.Split(string(logs), "\n")

	if lastMessage != "" {
		cut := 0
		for i, m := range res {
			if strings.Contains(m, lastMessage) {
				cut = i
				break
			}
		}
		if cut > 0 {
			res = res[:cut]
		}
	}

	return res, nil
}
