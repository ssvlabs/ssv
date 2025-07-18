package logs_catcher

import (
	"context"
	"encoding/binary"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
)

type mockDockerClient struct {
	ContainerRestartFunc func(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerListFunc    func(ctx context.Context, options types.ContainerListOptions) ([]types.Container, error)
	ContainerLogsFunc    func(ctx context.Context, container string, options types.ContainerLogsOptions) (io.ReadCloser, error)
}

func (mdc *mockDockerClient) ContainerRestart(
	ctx context.Context,
	containerID string,
	options container.StopOptions,
) error {
	if mdc.ContainerRestartFunc != nil {
		return mdc.ContainerRestartFunc(ctx, containerID, options)
	}
	return nil
}

func (mdc *mockDockerClient) ContainerList(
	ctx context.Context,
	options types.ContainerListOptions,
) ([]types.Container, error) {
	if mdc.ContainerListFunc != nil {
		return mdc.ContainerListFunc(ctx, options)
	}
	return nil, nil
}

func (mdc *mockDockerClient) ContainerLogs(
	ctx context.Context,
	container string,
	options types.ContainerLogsOptions,
) (io.ReadCloser, error) {
	if mdc.ContainerLogsFunc != nil {
		return mdc.ContainerLogsFunc(ctx, container, options)
	}
	return nil, nil
}

type mockedLogger struct {
	in chan string
}

func (ml *mockedLogger) Write(s string) {
	var msglen = make([]byte, 8)
	binary.BigEndian.PutUint32(msglen[4:], uint32(len(s)))
	ml.in <- string(msglen)
	ml.in <- s
}

func (ml *mockedLogger) Read(p []byte) (n int, err error) {
	msg := <-ml.in
	copy(p, msg)
	return len(msg), nil
}

func (ml *mockedLogger) Close() error {
	return nil
}

// TODO : rewrite test
//
//func Test_SetupLogsListener(t *testing.T) {
//	cfg := genTestConfig()
//
//	const numLogs = 30
//
//	fataletd := make(chan struct{}, 5)
//
//	ml := &mockedLogger{
//		make(chan string, 2),
//	}
//
//	dockerMock := &mockDockerClient{
//		ContainerLogsFunc: func(ctx context.Context, container string, options types.ContainerLogsOptions) (io.ReadCloser, error) {
//			return ml, nil
//		},
//		ContainerListFunc: func(ctx context.Context, options types.ContainerListOptions) ([]types.Container, error) {
//			return []types.Container{
//				{Names: []string{"ssv-node-1"}, ID: "1"},
//				{Names: []string{"ssv-node-2"}, ID: "2"},
//				{Names: []string{"ssv-node-3"}, ID: "3"},
//				{Names: []string{"ssv-node-4"}, ID: "4"},
//				{Names: []string{"ignoreme"}, ID: "5"},
//			}, nil
//		},
//	}
//
//	cfg.FatalerFunc = func(s string) {
//		fataletd <- struct{}{}
//	}
//
//	cfg.ApproverFunc = func(s string) {
//		fmt.Printf("Approved")
//	}
//
//	go func() {
//		for i := 0; i < numLogs; i++ {
//			ml.Write("{ \"M\": \"random msg\", \"Field\": \"randomfield\" }")
//			time.Sleep(100 * time.Millisecond)
//		}
//		ml.Write("{ \"M\": \"Fatal message\", \"Field\": \"Fatalism\" }")
//	}()
//
//	go Listen(cfg, dockerMock)
//
//	select {
//	case <-fataletd:
//		break
//	case <-time.After((100 * time.Millisecond) * (numLogs + 5)):
//		t.Fail()
//	}
//
//}
