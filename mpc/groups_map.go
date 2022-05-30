package mpc

import (
	"context"
	"github.com/bloxapp/ssv/validator"
	"go.uber.org/zap"
	"sync"
)

// groupIterator is the function used to iterate over existing groups
type groupIterator func(*Group) error

// groupsMap manages a collection of running mpc groups, similar to validatorsMap
type groupsMap struct {
	logger *zap.Logger
	ctx    context.Context

	optsTemplate *validator.Options

	lock          sync.RWMutex
	groupsMap map[string]*Group
}
