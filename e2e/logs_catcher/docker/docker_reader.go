package docker

import (
	"context"
	"encoding/binary"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"io"
)

type StreamCLI interface {
	ContainerLogs(ctx context.Context, container string, options types.ContainerLogsOptions) (io.ReadCloser, error)
}

func New() (*client.Client, error) {
	return client.NewClientWithOpts(client.FromEnv)
}

func StreamDockerLogs(ctx context.Context, cli StreamCLI, containerName string, logsChan chan string) error {
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
	hdr := make([]byte, 8)
	for {
		_, err := i.Read(hdr)
		if err != nil {
			return err
		}
		count := binary.BigEndian.Uint32(hdr[4:])
		dat := make([]byte, count)
		_, err = i.Read(dat)
		if err != nil {
			return err
		}
		logsChan <- string(dat)
	}
}
