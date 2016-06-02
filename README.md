# fluentd_monitor_agent_exporter

Export Fluentd monitor agent information.

# How to use

```
$ fluentd_exporter
  -fluentd.endpoint string
        Fluentd monitor agent endpoint. (default "http://localhost:24220")
  -fluentd.timeout duration
        Timeout for trying to get stats from Fluentd. (default 5s)
  -log.format value
        If set use a syslog logger or JSON logging. Example: logger:syslog?appname=bob&local=7 or logger:stdout?json=true. Defaults to stderr.
  -log.level value
        Only log messages with the given severity or above. Valid levels: [debug, info, warn, error, fatal]. (default info)
  -namespace string
        Namespace for metrics. (default "fluentd")
  -version
        Show version information
  -web.listen-address string
        Address to listen on for web interface and telemetry. (default ":9121")
  -web.telemetry-path string
        Path under which to expose metrics. (default "/metrics")
```

# LICENSE
MIT
