package logs_catcher

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/e2e/logs_catcher/docker"
	"github.com/bloxapp/ssv/e2e/logs_catcher/parser"
	"go.uber.org/zap"
)

const MesasgeKey = "msg" // todo: pass to struct/load from config

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
	ourm, ourok := lc.Fields[MesasgeKey]
	m, ok := fed[MesasgeKey] // todo: const/config message key name
	if ourok && ok {
		if !ourm(fmt.Sprint(m)) {
			return
		}
	}

	//	lc.logger.Info("message is accurate ", zap.String("orig", orig))

	if len(lc.Fields) > 0 {

		for k, v := range lc.Fields {
			mv, ok := fed[k]
			if !ok {
				return
			}
			if !v(fmt.Sprint(mv)) {
				return
			}
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

func Listen(ctx context.Context, logger *zap.Logger, cfg Config, cli DockerCLI) {
	ctx, c := context.WithCancel(ctx)
	defer c()

	var feeders []Feeder

	for i, ftl := range cfg.Fatalers {
		kvs := []KeyValue{}
		for k, v := range ftl {
			kvs = append(kvs, KV(k, v))
		}
		fatl := NewLogAction(logger, fmt.Sprint(i), cfg.FatalerFunc, kvs...)
		feeders = append(feeders, fatl)
	}

	for i, app := range cfg.Approvers {
		flds := make([]zap.Field, 0)
		for k, f := range app {
			flds = append(flds, zap.Any(k, f))
		}
		kvs := []KeyValue{}
		for k, v := range app {
			kvs = append(kvs, KV(k, v))
		}
		appr := NewLogAction(logger, fmt.Sprint(i), cfg.ApproverFunc, kvs...)
		feeders = append(feeders, appr)
	}

	ch := make(chan string, 1024)
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
	if err != nil {
		logger.Error("Log streaming stopped with err ", zap.Error(err))
		c()
		close(ch) // not writing since StreamDockerLogs is done
	}
}
