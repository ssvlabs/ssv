package logs_catcher

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/e2e/logs_catcher/docker"
	"github.com/bloxapp/ssv/e2e/logs_catcher/parser"
	"github.com/docker/docker/api/types"
	"log"
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

func (lc *LogCatcher) Feed(fed map[string]string) {
	ourm, ourok := lc.Fields["M"]
	m, ok := fed["M"] // todo: const/config

	if ourok && ok {
		if !ourm(m) {
			return
		}
	}

	for k, v := range lc.Fields {
		mv, ok := fed[k]
		if !ok {
			continue
		}
		if v(mv) {
			res := ""
			for kk, vv := range fed {
				res += kk + ": " + vv + " "
			}
			lc.Action(res)
		}
	}
}

type KeyValue struct {
	Key   string
	Value string
}

func KV(key, value string) KeyValue {
	return KeyValue{key, value}
}

func NewLogFataler(name string, kvs ...KeyValue) *LogCatcher {
	lc := &LogCatcher{
		Name:   fmt.Sprintf("%s Log Fataler", name),
		Fields: make(map[string]func(string) bool),
	}
	for _, kv := range kvs {
		lc.AddFieldCondition(kv.Key, EqualFunc(kv.Value))
	}
	lc.action = func(lc *LogCatcher, logEntry string) {
		log.Fatal(fmt.Errorf("fatal: %v", logEntry))
	}
	return lc
}

type Restarter func() error

func NewLogRestarter(name string, rs Restarter, kvs ...KeyValue) *LogCatcher {
	lc := &LogCatcher{
		Name:   fmt.Sprintf("%s Log Restarter", name),
		Fields: make(map[string]func(string) bool),
	}
	for _, kv := range kvs {
		lc.AddFieldCondition(kv.Key, EqualFunc(kv.Value))
	}
	lc.action = func(lc *LogCatcher, logEntry string) {
		log.Printf("restarting on: %v", logEntry)
		// todo: better err handling here
		if err := rs(); err != nil {
			panic(fmt.Errorf("can't restart err:%v", err))
		}
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
	Feed(fed map[string]string)
}

type DockerCLI interface {
	docker.RestartCLI
	docker.StreamCLI
}

func SetupLogsListener(cfg Config, cli DockerCLI) {
	ctx, c := context.WithCancel(context.Background())
	defer c()

	allDockers, err := docker.GetDockers(ctx, cli, func(container2 types.Container) bool {
		for _, nm := range container2.Names {
			for _, ign := range cfg.IgnoreContainers {
				if nm == ign {
					return false
				}
			}
		}
		return true
	})

	if err != nil {
		panic(fmt.Errorf("can't open docker client %v", err))
	}

	var feeders []Feeder

	for i, ftl := range cfg.Fatalers {
		kvs := []KeyValue{}
		for k, v := range ftl {
			kvs = append(kvs, KV(k, v))
		}
		fatl := NewLogFataler(fmt.Sprint(i), kvs...)
		feeders = append(feeders, fatl)
	}
	for i, rst := range cfg.Restarters {
		kvs := []KeyValue{}
		for k, v := range rst {
			kvs = append(kvs, KV(k, v))
		}
		rest := NewLogRestarter(
			fmt.Sprint(i),
			func() error { return docker.MassRestarter(ctx, cli, allDockers, nil) },
			kvs...)
		feeders = append(feeders, rest)
	}

	if err != nil {
		panic(err)
	}
	ch := make(chan string, 5)
	go func() {
		for log := range ch {
			p, err := parser.JSON(log)
			if err != nil {
				panic(err)
			}
			for _, feeder := range feeders {
				feeder.Feed(p)
			}
		}
	}()
	err = docker.StreamDockerLogs(ctx, cli, "logger-test", ch)
	if err != nil {
		panic(err)
	}
}
