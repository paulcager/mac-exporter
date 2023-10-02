package main

// #cgo LDFLAGS: -framework IOKit
//
// #include "smc.h"
// #include <math.h>
//
// io_connect_t conn;
//
//double SMCGetFloat(char* key)
//{
//    SMCVal_t val;
//    kern_return_t result;
//
//    result = SMCReadKey2(key, &val, conn);
//    if (result == kIOReturnSuccess) {
//       // read succeeded - check returned value
//       return getFloatFromVal(val);
//    }
//    // read failed
//    return NAN;
//}
//
//
import "C"

import (
	"fmt"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
	"net/http"
	"os"
	"sync"
	"time"
	"unsafe"

	log "github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/kr/pretty"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

const (
	namespace = "mac"
	subsystem = "power"
)

var (
	metricsEndpoint = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
	webConfig       = webflag.AddFlags(kingpin.CommandLine, ":9784")

	logger log.Logger

	_ = pretty.Print
)

type SensorCode struct {
	name string
	key  string
	cKey unsafe.Pointer
	desc *prometheus.Desc
}

var sensorCodes []SensorCode

func init() {
	add := func(key string, name string, description string) {
		sensorCodes = append(sensorCodes,
			SensorCode{
				name: name,
				key:  key,
				cKey: unsafe.Pointer(C.CString(key)),
				desc: prometheus.NewDesc(
					prometheus.BuildFQName(namespace, subsystem, name),
					description,
					nil,
					prometheus.Labels{"code": key},
				),
			},
		)
	}

	// See https://logi.wiki/index.php/SMC_Sensor_Codes for a list of codes.
	add("TC0P", "cpu_temp", "CPU temperature")
	add("TG0P", "gpu_temp", "GPU temperature")
	add("TM0P", "memory_temp", "RAM temperature")
	add("Ts0P", "palm_temp_left", "Left palm-pad temperature")
	add("Ts1P", "palm_temp_right", "Right palm-pad temperature")
	add("F0Ac", "fan_speed_left", "Left fan speed (units unknown)")
	add("F1Ac", "fan_speed_right", "Right fan speed (units unknown)")
	add("ID0R", "dc_in", "dc in ??")
	add("PPBR", "battery_current", "Batter current (A)")
	add("TaLC", "airflow_left", "Left airflow (units unknown)")
	add("TaRC", "airflow_right", "Right airflow (units unknown)")
	//add("", "")
}

func main() {
	C.SMCOpen(&C.conn)
	defer C.SMCClose(C.conn)

	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.Version(version.Print("mac_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	logger = promlog.New(promlogConfig)
	level.Info(logger).Log("msg", "Starting "+namespace+"_exporter", "version", version.Info())
	level.Info(logger).Log("msg", "Build context", "build_context", version.BuildContext())

	exporter := NewExporter(logger)
	prometheus.MustRegister(exporter)
	prometheus.MustRegister(version.NewCollector("mac_exporter"))

	http.Handle(*metricsEndpoint, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>Mac Exporter</title></head>
             <body>
             <h1>Mac Exporter</h1>
             <p><a href='` + *metricsEndpoint + `'>Metrics</a></p>
             </body>
             </html>`))
	})
	srv := &http.Server{}
	if err := web.ListenAndServe(srv, webConfig, logger); err != nil {
		level.Error(logger).Log("msg", "Error starting HTTP server", "err", err)
		os.Exit(1)
	}

	for {
		fmt.Println()

		for _, sensor := range sensorCodes {
			fmt.Println(sensor.name, C.SMCGetFloat((*C.char)(sensor.cKey)))
		}
		time.Sleep(10 * time.Second)
	}
}

type Exporter struct {
	mutex sync.RWMutex

	totalScrapes prometheus.Counter
	logger       log.Logger
}

func NewExporter(logger log.Logger) *Exporter {
	return &Exporter{
		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "exporter_scrapes_total",
			Help:      "Current total scrapes.",
		}),
		logger: logger,
	}
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.totalScrapes.Desc()

	for _, sensor := range sensorCodes {
		ch <- sensor.desc
	}
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.mutex.Lock() // To protect metrics from concurrent collects.
	defer e.mutex.Unlock()

	e.totalScrapes.Inc()
	ch <- e.totalScrapes

	for _, sensor := range sensorCodes {
		value := float64(C.SMCGetFloat((*C.char)(sensor.cKey)))
		ch <- prometheus.MustNewConstMetric(sensor.desc, prometheus.GaugeValue, value)
	}
}
