package logs_catcher

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/e2e/logs_catcher/docker"
	"github.com/bloxapp/ssv/e2e/logs_catcher/parser"
)

type LogCatcher struct {
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

func (lc *LogCatcher) Feed(orig string, fed map[string]string) {
	ourm, ourok := lc.Fields["M"]
	m, ok := fed["M"] // todo: const/config message key name

	if ourok && ok {
		if !ourm(m) {
			return
		}
	}

	if len(lc.Fields) > 0 {

		for k, v := range lc.Fields {
			mv, ok := fed[k]
			if !ok {
				return
			}
			if !v(mv) {
				return
			}
		}

	}

	lc.Action(orig)
}

type KeyValue struct {
	Key   string
	Value string
}

func KV(key, value string) KeyValue {
	return KeyValue{key, value}
}

func NewLogAction(name string, action func(string), kvs ...KeyValue) *LogCatcher {
	lc := &LogCatcher{
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

func EqualFunc(val string) func(s string) bool {
	return func(s string) bool {
		if s == val {
			return true
		}
		return false
	}
}

type Feeder interface {
	Feed(orig string, fed map[string]string)
}

type DockerCLI interface {
	docker.Streamer
	docker.Lister
}

func Listen(cfg Config, cli DockerCLI) {
	ctx, c := context.WithCancel(context.Background())
	defer c()

	var feeders []Feeder

	for i, ftl := range cfg.Fatalers {
		kvs := []KeyValue{}
		for k, v := range ftl {
			kvs = append(kvs, KV(k, v))
		}
		fatl := NewLogAction(fmt.Sprint(i), cfg.FatalerFunc, kvs...)
		feeders = append(feeders, fatl)
	}

	for i, app := range cfg.Approvers {
		kvs := []KeyValue{}
		for k, v := range app {
			kvs = append(kvs, KV(k, v))
		}
		appr := NewLogAction(fmt.Sprint(i), cfg.ApproverFunc, kvs...)
		feeders = append(feeders, appr)
	}

	ch := make(chan string, 1024)
	go func() {
		for log := range ch {
			p, err := parser.JSON(log)
			if err != nil {
				panic(err)
			}
			for _, feeder := range feeders {
				feeder.Feed(log, p)
			}
		}
	}()
	// TODO: either apply logs collection on each container or fan in the containers to one log stream
	err := docker.StreamDockerLogs(ctx, cli, "logger-test", ch)
	if err != nil {
		panic(err)
	}
}
