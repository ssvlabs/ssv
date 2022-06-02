package mpc

import (
	"context"
	"go.uber.org/zap"
	"sync"
)

// groupIterator is the function used to iterate over existing groups
type groupIterator func(*Group) error

// groupsMap manages a collection of running mpc groups, similar to validatorsMap
type groupsMap struct {
	logger *zap.Logger
	ctx    context.Context

	optsTemplate *Options

	lock      sync.RWMutex
	groupsMap map[string]*Group
}

func newGroupsMap(ctx context.Context, logger *zap.Logger, optsTemplate *Options) *groupsMap {
	vm := groupsMap{
		logger:       logger.With(zap.String("component", "groupsMap")),
		ctx:          ctx,
		lock:         sync.RWMutex{},
		groupsMap:    make(map[string]*Group),
		optsTemplate: optsTemplate,
	}

	return &vm
}
