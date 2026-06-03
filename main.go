package main

// #cgo LDFLAGS: -framework IOKit
//
// #include "smc.h"
// #include <math.h>
// #include <CoreFoundation/CoreFoundation.h>
// #include <CoreFoundation/CFDateFormatter.h>
// #include <IOKit/pwr_mgt/IOPM.h>
// #include <IOKit/pwr_mgt/IOPMLib.h>
//
// io_connect_t conn;
//
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
// // TODO. This is bodgy (although I know we only get 3 elements in real life).
// #define MAX_ELEMENTS 32
// CFStringRef             keys[MAX_ELEMENTS];
// CFNumberRef             values[MAX_ELEMENTS];
//
// int power_status(void) {
//	CFDictionaryRef         cpuStatus;
//	IOReturn ret = IOPMCopyCPUPowerStatus(&cpuStatus);
//	int count;
// 	if (ret != kIOReturnSuccess) {
//		fprintf(stderr, "IOPMCopyCPUPowerStatus: %ld\n", (long)ret);
//		return 0;
//	}
//
//	count = CFDictionaryGetCount(cpuStatus);
//  if (count > MAX_ELEMENTS) {
//		fprintf(stderr, "IOPMCopyCPUPowerStatus returned %ld elements\n", (long)count);
//		return 0;
//	}
//
//  CFDictionaryGetKeysAndValues(cpuStatus,(const void **)keys, (const void **)values);
//
//	return count;
// }
//
// #define MAX_KEY_LEN 125
// char key_buff[MAX_KEY_LEN+1];
//
// char *get_power_key(int i) {
//	CFStringGetCString(keys[i], key_buff, MAX_KEY_LEN, kCFStringEncodingUTF8);
//	return key_buff;
// }
//
// int get_power_value(int i) {
//	int val;
//	CFNumberGetValue(values[i], kCFNumberIntType, &val);
//	return val;
// }
//
// io_service_t battery;
//
// int battery_open(void) {
//	battery = IOServiceGetMatchingService(kIOMasterPortDefault, IOServiceMatching("AppleSmartBattery"));
//	return battery != 0;
// }
//
// // Returns the named AppleSmartBattery property as a double (booleans as 0/1),
// // or NAN if the property is missing or not a number/boolean.
// double battery_prop(char *name) {
//	CFStringRef key = CFStringCreateWithCString(NULL, name, kCFStringEncodingUTF8);
//	CFTypeRef val = IORegistryEntryCreateCFProperty(battery, key, kCFAllocatorDefault, 0);
//	double out = NAN;
//	CFRelease(key);
//	if (val == NULL) {
//		return NAN;
//	}
//	if (CFGetTypeID(val) == CFNumberGetTypeID()) {
//		CFNumberGetValue(val, kCFNumberDoubleType, &out);
//	} else if (CFGetTypeID(val) == CFBooleanGetTypeID()) {
//		out = CFBooleanGetValue(val) ? 1 : 0;
//	}
//	CFRelease(val);
//	return out;
// }
import "C"

import (
	"math"
	"net/http"
	"os"
	"sync"
	"unsafe"

	log "github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
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
)

type SensorCode struct {
	name string
	key  string
	cKey unsafe.Pointer
	desc *prometheus.Desc
}

var sensorCodes []SensorCode

type BatteryProp struct {
	prop  string
	cProp unsafe.Pointer
	scale float64
	desc  *prometheus.Desc
}

var batteryProps []BatteryProp
var batteryOK bool

var powerStatusDesc = prometheus.NewDesc(
	prometheus.BuildFQName(namespace, subsystem, "status"),
	"MAC Power status",
	[]string{"name"},
	nil,
)

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
	add("TB0T", "battery_temp_0", "Battery sensor 0 temperature")
	add("TB1T", "battery_temp_1", "Battery sensor 1 temperature")
	add("TB2T", "battery_temp_2", "Battery sensor 2 temperature")
	add("Th0H", "heatsink_temp_0", "Heatsink 0 temperature")
	add("Th1H", "heatsink_temp_1", "Heatsink 1 temperature")
	add("TA0P", "ambient_temp", "Ambient temperature")
	add("TW0P", "wifi_temp", "WiFi module temperature")
	add("TI0P", "thunderbolt_temp", "Thunderbolt temperature")
	add("PSTR", "system_power", "Total system power (W)")
	add("PC0C", "cpu_core_power", "CPU core power (W)")
	add("PCPC", "cpu_package_power", "CPU package power (W)")
	add("PDTR", "dc_in_power", "DC-in power (W)")
	add("VD0R", "dc_in_voltage", "DC-in voltage (V)")
	//add("", "")

	addBattery := func(prop string, name string, scale float64, description string) {
		batteryProps = append(batteryProps,
			BatteryProp{
				prop:  prop,
				cProp: unsafe.Pointer(C.CString(prop)),
				scale: scale,
				desc: prometheus.NewDesc(
					prometheus.BuildFQName(namespace, "battery", name),
					description,
					nil,
					nil,
				),
			},
		)
	}

	// AppleSmartBattery properties (see `ioreg -rn AppleSmartBattery`).
	addBattery("CurrentCapacity", "current_capacity_mah", 1, "Current battery charge (mAh)")
	addBattery("MaxCapacity", "max_capacity_mah", 1, "Current full-charge capacity (mAh)")
	addBattery("DesignCapacity", "design_capacity_mah", 1, "Design full-charge capacity (mAh)")
	addBattery("CycleCount", "cycle_count", 1, "Battery charge cycles")
	addBattery("Temperature", "temp_celsius", 0.01, "Battery temperature (C)")
	addBattery("Voltage", "voltage_volts", 0.001, "Battery voltage (V)")
	addBattery("Amperage", "amperage_amps", 0.001, "Battery current; negative when discharging (A)")
	addBattery("IsCharging", "charging", 1, "1 if the battery is charging")
	addBattery("ExternalConnected", "external_power", 1, "1 if external power is connected")
	addBattery("FullyCharged", "fully_charged", 1, "1 if the battery is fully charged")
}

func main() {
	C.SMCOpen(&C.conn)
	defer C.SMCClose(C.conn)

	batteryOK = C.battery_open() != 0

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
}

func powerStatus() map[string]int {
	count := (int)(C.power_status())
	m := make(map[string]int)

	for i := 0; i < count; i++ {
		m[C.GoString(C.get_power_key(C.int(i)))] = (int)(C.get_power_value(C.int(i)))
	}

	return m
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

	for _, prop := range batteryProps {
		ch <- prop.desc
	}

	ch <- powerStatusDesc
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.mutex.Lock() // To protect metrics from concurrent collects.
	defer e.mutex.Unlock()

	e.totalScrapes.Inc()
	ch <- e.totalScrapes

	for _, sensor := range sensorCodes {
		value := float64(C.SMCGetFloat((*C.char)(sensor.cKey)))
		if !math.IsNaN(value) {
			ch <- prometheus.MustNewConstMetric(sensor.desc, prometheus.GaugeValue, value)
		}
	}

	if batteryOK {
		for _, prop := range batteryProps {
			value := float64(C.battery_prop((*C.char)(prop.cProp)))
			if !math.IsNaN(value) {
				ch <- prometheus.MustNewConstMetric(prop.desc, prometheus.GaugeValue, value*prop.scale)
			}
		}
	}

	for k, v := range powerStatus() {
		ch <- prometheus.MustNewConstMetric(powerStatusDesc, prometheus.GaugeValue, float64(v), k)
	}
}
