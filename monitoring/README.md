[<img src="../docs/resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>


# SSV - Monitoring

This page will outline how to monitor an SSV Node using Grafana and Prometheus.
### Pre-requisites
Make sure your node is exposing a `/metrics` and `/health` endpoints. This is done via node configuration, as explained in the [Installation guide on the docs](https://docs.ssv.network/run-a-node/operator-node/installation#create-configuration-file).

This guide will not go into the details of setting up and running Prometheus or Grafana. For this, we recommend visiting their related documentations:

[Prometheus docs](https://prometheus.io/docs/introduction/overview/)

[Grafana docs](https://grafana.com/docs/)

For Grafana, specifically, [Grafana Cloud](https://grafana.com/docs/grafana-cloud/) is a viable solution, especially for beginners.

See the configuration of a [local prometheus service](prometheus/prometheus.yaml).

### Health Check

Even if Prometheus is not configured, the `/health` end-point can simply be polled by a simple HTTP client as a health check. \
In case the node is healthy it returns an HTTP Code `200` with an empty response:
```shell
$ curl http://localhost:15000/health
```

If the node is not healthy, the corresponding errors will be returned with HTTP a Code of `500`:
```shell
$ curl http://localhost:15000/health
{"errors": ["could not sync eth1 events"]}
```

## Prometheus

In a typical setup, where only one SSV node Docker container is running, Prometheus should be configured with a file like this:

```yaml
global:
  scrape_interval:     10s
  evaluation_interval: 10s

scrape_configs:
  - job_name: ssv
    metrics_path: /metrics
    static_configs:
      - targets:
        # change the targets according to your setup
        # if running prometheus from source, or as executable:
        # - <container_name>:15000 (i.e.: ssv_node:15000, check with docker ps command)
        # if running prometheus as docker container:
        - host.docker.internal:15000
  - job_name: ssv_health
    metrics_path: /health
    static_configs:
      - targets:
        # change the targets according to your setup
        # if running prometheus from source, or as executable:
        # - <container_name>:15000 (i.e.: ssv_node:15000, check with docker ps command)
        # if running prometheus as docker container:
        - host.docker.internal:15000

```

And to launch the Prometheus service as a Docker container as well ([using the official Docker image, as shown here](https://hub.docker.com/r/prom/prometheus)), use this command, where `/path/to/prometheus.yml` is the path and filename of the configuration file described above:

```bash
docker run \
    -p 9090:9090 \
    -v /path/to/prometheus.yml:/etc/prometheus/prometheus.yml \
    prom/prometheus
```


> ⚠️ Note: If you are not running Prometheus as a Docker container, but as an executable, change the `targets` in the config file to reflect the correct networking connections. In the case where the SSV Node container is called `ssv_node` the targets should look like this:

```yaml
      - targets:
        - ssv_node:15000
```

> Use the `docker ps` command to verify the name of the SSV Node container.

## Grafana monitoring

After successfully configuring a Prometheus service, and [adding it as a data source to Grafana](https://grafana.com/docs/grafana/latest/datasources/prometheus/configure-prometheus-data-source/) (read [here for Grafana Cloud](https://grafana.com/docs/grafana-cloud/connect-externally-hosted/data-sources/prometheus/configure-prometheus-data-source/)), a Grafana dashboard can be created.

Below, an example of two dashboards, respectively monitoring the SSV Node and the performance of an Operator:

* [SSV Node monitoring](grafana/dashboard_ssv_node.json)
* [Operator performance monitoring](grafana/dashboard_ssv_operator_performance.json.json)

The dashboards leverage Grafana templating so that one can select different datasources, the Grafana SSV operators are inferred from the Prometheus metrics, so if you spin up more SSV operators, they will show up on the dashboard seamlessly. 

--- 
## Profiling

Profiling can be enabled in the node configuration file (`config.yaml`):
```yaml
EnableProfile: true
```
> Note: remember to restart the node after changing its configuration

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
