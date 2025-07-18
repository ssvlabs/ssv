package docker

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
)

type Lister interface {
	//	ContainerRestart(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerList(
		ctx context.Context,
		options types.ContainerListOptions,
	) ([]types.Container, error)
}

//
//func Restart(ctx context.Context, c Lister, containerName string) error {
//	return c.ContainerRestart(ctx, containerName, container.StopOptions{
//		Signal:  "",
//		Timeout: nil,
//	})
//}
//
//func MassRestarter(ctx context.Context, cl Lister, containers []string, ignore []string) error {
//contLoop:
//	for _, c := range containers {
//		for _, i := range ignore {
//			if c == i {
//				continue contLoop
//			}
//		}
//		if err := Restart(ctx, cl, c); err != nil {
//			return err
//		}
//
//	}
//	return nil
//}

func GetDockers(
	ctx context.Context,
	c Lister,
	filts ...func(container2 types.Container) bool,
) ([]string, error) {
	cs, err := c.ContainerList(ctx, types.ContainerListOptions{
		Size:    false,
		All:     false,
		Latest:  false,
		Since:   "",
		Before:  "",
		Limit:   0,
		Filters: filters.Args{},
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)

	for i := 0; i < len(cs); i++ {
		toAdd := false
		if len(filts) == 0 {
			toAdd = true
		} else {
			for _, filt := range filts {
				if filt(cs[i]) {
					toAdd = true
					break
				}
			}
		}
		if toAdd {
			ids = append(ids, cs[i].ID)
		}
	}
	return ids, nil
}
