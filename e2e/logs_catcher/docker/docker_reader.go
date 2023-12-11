package docker

import (
	"context"
	"encoding/binary"
	"github.com/bloxapp/ssv/e2e/logs_catcher/logs"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"io"
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
		select {
		case logsChan <- string(dat):
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func DockerLogs(
	ctx context.Context,
	cli Streamer,
	containerName string,
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
	hdr := make([]byte, 8)
	res := make([]string, 0)
	for {
		_, err := i.Read(hdr)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		count := binary.BigEndian.Uint32(hdr[4:])
		dat := make([]byte, count)
		_, err = i.Read(dat)
		if err != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			break
		}
		res = append(res, string(dat))
	}
	return res, nil
}
