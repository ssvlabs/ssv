[<img src="../docs/resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>

# SSV - Profiling

Profiling needs to be enabled via config:
```yaml
EnableProfile: true
```

### Usage

All the default `pprof` routes are available via HTTP:
```shell
$ curl http://localhost:15000/debug/pprof/goroutine?minutes\=20 --output goroutines.tar.gz
```

Open with Go CLI:
```shell
$ go tool pprof goroutines.tar.gz
```

Or with Web UI:
```shell
$ go tool pprof -web goroutines.tar.gz
```

Another option is to visualize results in web UI directly:
```shell
$ go tool pprof -web http://localhost:15001/debug/pprof/heap?minutes=5
```
