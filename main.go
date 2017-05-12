package main

import (
	"flag"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"net"
	"net/http"
	"sync"
	"time"
	"io/ioutil"
	"encoding/json"
)

var (
	VERSION = "0.0.1"

	showVersion = flag.Bool("version", false, "Show version information")
	namespace = flag.String("namespace", "fluentd", "Namespace for metrics.")
	listenAddress = flag.String("web.listen-address", ":9224", "Address to listen on for web interface and telemetry.")
	metricPath = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
	endpoint = flag.String("fluentd.endpoint", "http://localhost:24220", "Fluentd monitor agent endpoint.")
	timeout = flag.Duration("fluentd.timeout", 5 * time.Second, "Timeout for trying to get stats from Fluentd.")
)

type Exporter struct {
	endpoint          string
	namespace         string
	client            *http.Client

	duration          prometheus.Gauge
	totalScrapes      prometheus.Counter
	error             prometheus.Gauge
	totalErrors       prometheus.Counter

	bufQueueLength    *prometheus.GaugeVec // buffer_queue_length
	bufTotalQueueSize *prometheus.GaugeVec // buffer_total_queued_size
	retryCount        *prometheus.GaugeVec // retry_count

	sync.RWMutex
}

func NewExporter(endpoint string, namespace string, timeout time.Duration) *Exporter {
	e := Exporter{
		endpoint:  endpoint,
		namespace: namespace,
		client: &http.Client{
			Transport: &http.Transport{
				Dial: func(netw, addr string) (net.Conn, error) {
					c, err := net.DialTimeout(netw, addr, timeout)
					if err != nil {
						return nil, err
					}
					if err := c.SetDeadline(time.Now().Add(timeout)); err != nil {
						return nil, err
					}
					return c, nil
				},
			},
		},
		duration: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "last_scrape_duration_seconds",
			Help:      "Duration of the last scrape of metrics from Fluentd.",
		}),
		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "scrapes_total",
			Help:      "Total number of times Fluentd was scraped for metrics.",
		}),
		error: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "last_scrape_error",
			Help:      "Whether the last scrape of metrics from Fluentd resulted in an error (1 for error, 0 for success).",
		}),
		totalErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "scrape_errors_total",
			Help:      "Total count of error scraping Fluentd.",
		}),
		bufQueueLength: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "buffer_queue_length",
			Help:      "buffer_queue_length",
		}, []string{"pluginType", "pluginId"}),
		bufTotalQueueSize: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "buffer_total_queued_size",
			Help:      "buffer_total_queued_size",
		}, []string{"pluginType", "pluginId"}),
		retryCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "retry_count",
			Help:      "retry_count",
		}, []string{"pluginType", "pluginId"}),
	}

	return &e
}

func (e *Exporter) Describe(ch chan <- *prometheus.Desc) {
	ch <- e.duration.Desc()
	ch <- e.totalScrapes.Desc()
	ch <- e.error.Desc()

	e.bufQueueLength.Describe(ch);
	e.bufTotalQueueSize.Describe(ch);
	e.retryCount.Describe(ch);
}

func (e *Exporter) Collect(ch chan <- prometheus.Metric) {
	e.Lock()
	defer e.Unlock()

	pluginChan := make(chan plugin)
	go e.scrape(pluginChan)
	e.setMetrics(pluginChan)

	ch <- e.duration
	ch <- e.totalScrapes
	ch <- e.error
	ch <- e.totalErrors

	e.bufQueueLength.Collect(ch)
	e.bufTotalQueueSize.Collect(ch)
	e.retryCount.Collect(ch)
}

func (e *Exporter) fetch() ([]byte, error) {
	res, err := e.client.Get(e.endpoint + "/api/plugins.json")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if !(res.StatusCode >= 200 && res.StatusCode < 300) {
		return nil, err
	}

	bodyByte, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	return bodyByte, nil
}

func (e *Exporter) scrape(pluginChan chan <- plugin) {
	defer close(pluginChan)
	now := time.Now().UnixNano()
	e.totalScrapes.Inc()
	error := 0

	bodyBytes, err := e.fetch();
	if err != nil {
		log.Errorf("Failed to fetch json. %s", err)
		error = 1
	} else {
		var body pluginsBody
		err = json.Unmarshal(bodyBytes, &body)
		if err != nil {
			log.Errorf("Failed to decode json. %s", err)
			error = 1
		} else {
			for _, plugin := range body.Plugins {
				if plugin.OutputPlugin {
					pluginChan <- plugin
				}
			}
		}
	}

	e.error.Set(float64(error))
	if error == 1 {
		e.totalErrors.Inc()
	}
	e.duration.Set(float64(time.Now().UnixNano() - now) / 1000000000)
}

func (e *Exporter) setMetrics(pluginChan <-chan plugin) {
	for plugin := range pluginChan {
		var labels prometheus.Labels = map[string]string{
			"pluginType": plugin.PluginType,
			"pluginId": plugin.PluginId,
		}

		e.bufQueueLength.With(labels).Set(float64(plugin.BufQueueLength))
		e.bufTotalQueueSize.With(labels).Set(float64(plugin.BufTotalQueuedSize))
		e.retryCount.With(labels).Set(float64(plugin.RetryCount))
	}
}

type pluginsBody struct {
	Plugins []plugin `json:"plugins"`
}

type plugin struct {
	PluginId           string `json:"plugin_id"`
	PluginType         string `json:"type"`
	OutputPlugin       bool `json:"output_plugin"`
	BufQueueLength     float64 `json:"buffer_queue_length"`
	BufTotalQueuedSize float64 `json:"buffer_total_queued_size"`
	RetryCount         float64 `json:"retry_count"`
}

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("Fluentd monitor agent exporter v%s\n", VERSION)
		return
	}

	exporter := NewExporter(*endpoint, *namespace, *timeout)
	prometheus.MustRegister(exporter)

	http.Handle(*metricPath, prometheus.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
<head><title>Fluentd monitor agent exporter</title></head>
<body>
<h1>Fluentd monitor agent exporter</h1>
<p><a href='` + *metricPath + `'>Metrics</a></p>
</body>
</html>`))
	})

	log.Infof("providing metrics at %s%s", *listenAddress, *metricPath)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
