package mpc

import (
	"context"
	"fmt"
	storage "github.com/bloxapp/ssv/mpc/storage"
	"go.uber.org/zap"
	"math/big"
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

// GetMpcGroup returns an mpc group
func (m *groupsMap) GetMpcGroup(requestId *big.Int) (*Group, bool) {
	// main lock
	m.lock.RLock()
	defer m.lock.RUnlock()

	v, ok := m.groupsMap[requestId.String()]

	return v, ok
}

// GetOrCreateMpcGroup creates a new mpc group instance if not exist
func (m *groupsMap) GetOrCreateMpcGroup(request *storage.DkgRequest) *Group {
	// main lock
	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.groupsMap[request.Id.String()]; !ok {
		opts := *m.optsTemplate
		opts.Request = request
		m.groupsMap[request.Id.String()] = New(opts)
		printRequest(request, m.logger, "setup mpc group done")
		opts.Request = nil
	} else {
		printRequest(request, m.logger, "get mpc group")
	}

	return m.groupsMap[request.Id.String()]
}

// Size returns the number of validators in the map
func (m *groupsMap) Size() int {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return len(m.groupsMap)
}

func printRequest(r *storage.DkgRequest, logger *zap.Logger, msg string) {
	var operators []string
	for _, o := range r.Operators {
		operators = append(operators, fmt.Sprintf(`[PK=%x]`, o))
	}
	logger.Debug(msg,
		zap.String("requestId", r.Id.String()),
		zap.Uint64("nodeID", r.NodeID),
		zap.Strings("operators", operators))
}
