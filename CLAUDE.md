# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Prometheus exporter for macOS hardware sensors (temperatures, fan speeds, battery current) read via Apple's SMC (System Management Controller), plus CPU power status from IOKit's power-management API. It serves metrics on port `:9784` at `/metrics`, namespaced `mac_power_*`.

macOS-only: it links against IOKit via cgo and cannot be built or run elsewhere.

## Build and run

```bash
go build          # produces ./mac-exporter (cgo; requires Xcode command-line tools)
./mac-exporter    # listens on :9784; --web.telemetry-path and exporter-toolkit web flags available
```

There are no tests.

## Architecture

Three layers, all in a handful of files:

- `smc.c` / `smc.h` — third-party (GPL) SMC access code. Opens an IOKit connection to `AppleSMC` and reads typed sensor values by 4-char key (e.g. `TC0P`), decoding the various SMC fixed-point data types to float.
- `main.go` cgo preamble (the comment block above `import "C"`) — glue C code: `SMCGetFloat(key)` wrapping `SMCReadKey2`, `power_status()`/`get_power_key()`/`get_power_value()` wrapping `IOPMCopyCPUPowerStatus`, and `battery_open()`/`battery_prop(name)` reading `AppleSmartBattery` registry properties. Note this C lives in Go comments — edits there must keep valid cgo comment syntax.
- `main.go` Go code — a custom `prometheus.Collector` (`Exporter`) that reads each sensor on every scrape. SMC sensors are registered in `init()` via the `add(key, name, description)` helper (key reference: https://logi.wiki/index.php/SMC_Sensor_Codes); battery properties via `addBattery(prop, name, scale, description)`, which scales raw units (e.g. mV → V) and exports as `mac_battery_*`. Power-status entries become a single `mac_power_status` gauge with a `name` label.

Sensors/properties that don't exist on the current machine return NaN and are skipped at collect time, so speculative keys are harmless to add.

## Notes

- A single global SMC connection (`C.conn`) is opened in `main()` and shared; `Collect` is serialized with a mutex.
- The committed binary `mac-exporter` and `nohup.out` are gitignored build/run artifacts.
