# mac-exporter

A Prometheus exporter for macOS hardware sensors (temperatures, fan speeds,
power draw, CPU throttling status, battery charge/health), read from the SMC
and IOKit.

Listens on `:9784`, metrics at `/metrics`, namespaces `mac_power_*` and
`mac_battery_*`. OS-level metrics (CPU, memory, disk, network) are out of
scope — use node_exporter for those.

## Build

```bash
go build    # macOS only (cgo, links against IOKit)
```

## Run at login (launchd)

Create `~/Library/LaunchAgents/com.paulcager.mac-exporter.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.paulcager.mac-exporter</string>
    <key>ProgramArguments</key>
    <array>
        <string>/Users/pcager/git/mac-exporter/mac-exporter</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/Users/pcager/Library/Logs/mac-exporter.log</string>
    <key>StandardErrorPath</key>
    <string>/Users/pcager/Library/Logs/mac-exporter.err.log</string>
</dict>
</plist>
```

Then:

```bash
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.paulcager.mac-exporter.plist
```

Management:

```bash
launchctl print gui/$(id -u)/com.paulcager.mac-exporter        # status
launchctl kickstart -k gui/$(id -u)/com.paulcager.mac-exporter # restart (after go build)
launchctl bootout gui/$(id -u)/com.paulcager.mac-exporter      # stop and unload
```

## Log rotation

launchd does not rotate the log files. The exporter only logs at startup
and on errors, so growth is slow, but it is unbounded. To rotate, create
`/etc/newsyslog.d/mac-exporter.conf` (needs sudo):

```
# logfilename                                  [owner:group]  mode count size(KB) when flags
/Users/pcager/Library/Logs/mac-exporter.log     pcager:staff  644  3     1024     *    J
/Users/pcager/Library/Logs/mac-exporter.err.log pcager:staff  644  3     1024     *    J
```

This keeps 3 compressed (`J` = bzip2) generations, rotating whenever a file
exceeds 1MB. newsyslog runs hourly via launchd; no further setup needed.
