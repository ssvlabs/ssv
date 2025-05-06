package logs_catcher

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/e2e/logs_catcher/docker"
	"github.com/ssvlabs/ssv/e2e/logs_catcher/parser"
)

const MessageKey = "msg" // todo: pass to struct/load from config

type LogCatcher struct {
	logger     *zap.Logger
	Name       string
	LogMessage string
	Fields     map[string]func(string) bool // key-value checker
	action     func(lc *LogCatcher, logEntry string)
}

func (lc *LogCatcher) Action(logEntry string) {
	lc.action(lc, logEntry)
}

func (lc *LogCatcher) AddFieldCondition(key string, finder func(string) bool) {
	lc.Fields[key] = finder
}

func (lc *LogCatcher) Feed(orig string, fed map[string]any) {
	ourm, ourok := lc.Fields[MessageKey]
	m, ok := fed[MessageKey] // todo: const/config message key name
	if ourok && ok {
		if !ourm(fmt.Sprint(m)) {
			return
		}
	}

	//	lc.logger.Info("message is accurate ", zap.String("orig", orig))

	for k, v := range lc.Fields {
		if k == MessageKey {
			continue
		}
		mv, ok := fed[k]
		if !ok {
			return
		}
		if !v(fmt.Sprint(mv)) {
			return
		}
	}

	//	lc.logger.Info("running action ", zap.String("orig", orig))

	lc.Action(orig)
}

type KeyValue struct {
	Key   string
	Value any
}

func KV(key string, value any) KeyValue {
	return KeyValue{key, value}
}

func NewLogAction(logger *zap.Logger, name string, action func(string), kvs ...KeyValue) *LogCatcher {
	lc := &LogCatcher{
		logger: logger,
		Name:   fmt.Sprintf("Log %s", name),
		Fields: make(map[string]func(string) bool),
	}
	for _, kv := range kvs {
		lc.AddFieldCondition(kv.Key, EqualFunc(kv.Value))
	}
	lc.action = func(lc *LogCatcher, logEntry string) {
		action(logEntry)
	}
	return lc
}

func EqualFunc(val any) func(s string) bool {
	return func(s string) bool {
		if s == fmt.Sprint(val) {
			return true
		}
		return false
	}
}

type Feeder interface {
	Feed(orig string, fed map[string]any)
}

type DockerCLI interface {
	docker.Streamer
	docker.Lister
}

func FatalListener(ctx context.Context, logger *zap.Logger, cli DockerCLI) error {
	ctx, c := context.WithCancel(ctx)
	defer c()

	errChan := make(chan error, 2)

	var feeders []Feeder

	stopCond := []KeyValue{KV(MessageKey, waitFor)}
	stopCondAction := func(s string) {
		logger.Info("Stop condition arrived, stopping error checks")
		errChan <- nil
		c()
	}
	stp := NewLogAction(logger, fmt.Sprint(0), stopCondAction, stopCond...)

	feeders = append(feeders, stp)

	kvs := []KeyValue{KV("level", "error")}
	fatl := NewLogAction(logger, fmt.Sprint(0), func(s string) {
		errChan <- fmt.Errorf("found fatal: %v", s)
		c()
	}, kvs...)

	feeders = append(feeders, fatl)

	ch := make(chan string, 1024)
	defer close(ch)

	go func() {
		for log := range ch {
			p, err := parser.JSON(log)
			if err != nil {
				logger.Error("failed to parse log", zap.Error(err), zap.String("log", log))
				continue
			}
			for _, feeder := range feeders {
				feeder.Feed(log, p)
			}
		}
	}()
	// TODO: either apply logs collection on each container or fan in the containers to one log stream
	err := docker.StreamDockerLogs(ctx, cli, "beacon_proxy", ch)
	if err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("Log streaming stopped with err ", zap.Error(err))
		c()
		errChan <- err
	}

	return <-errChan
}
