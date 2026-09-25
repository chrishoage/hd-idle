// hd-idle - spin down idle hard disks
// Copyright (C) 2018  Andoni del Olmo
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"fmt"
	"github.com/adelolmo/hd-idle/diskstats"
	"github.com/adelolmo/hd-idle/io"
	"github.com/adelolmo/hd-idle/sgio"
	"log"
	"math"
	"os"
	"time"
)

const (
	SCSI       = "scsi"
	ATA        = "ata"
	dateFormat = "2006-01-02T15:04:05"
)

type DefaultConf struct {
	Idle                    time.Duration
	CommandType             string
	PowerCondition          uint8
	Debug                   bool
	LogFile                 string
	SymlinkPolicy           int
	IgnoreSpinDownDetection bool
}

type DeviceConf struct {
	Name           string
	GivenName      string
	Idle           time.Duration
	CommandType    string
	PowerCondition uint8
}

type Config struct {
	Devices    []DeviceConf
	Defaults   DefaultConf
	SkewTime   time.Duration
	NameMap    map[string]string
	WakeWindow *WakeWindow
	WakeIdle   *time.Duration
}

type WakeWindow struct {
	StartMinute int
	EndMinute   int
}

func (w *WakeWindow) contains(at time.Time) bool {
	day := time.Date(at.Year(), at.Month(), at.Day(), 12, 0, 0, 0, at.Location())
	if w.StartMinute < w.EndMinute {
		return w.containsDay(at, day)
	}
	return w.containsDay(at, day.AddDate(0, 0, -1)) || w.containsDay(at, day)
}

func (w *WakeWindow) containsDay(at, day time.Time) bool {
	start := wakeWindowBoundary(day, w.StartMinute, false)
	endDay := day
	if w.StartMinute > w.EndMinute {
		endDay = day.AddDate(0, 0, 1)
	}
	end := wakeWindowBoundary(endDay, w.EndMinute, true)
	return !at.Before(start) && at.Before(end)
}

func wakeWindowBoundary(day time.Time, minute int, last bool) time.Time {
	location := day.Location()
	year, month, date := day.Date()
	target := time.Date(year, month, date, minute/60, minute%60, 0, 0, time.UTC)
	dayStart := time.Date(year, month, date, 0, 0, 0, 0, location)
	nextDay := day.AddDate(0, 0, 1)
	nextStart := time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), 0, 0, 0, 0, location)
	var boundary time.Time
	for probe := dayStart.Add(-3 * time.Hour); probe.Before(nextStart.Add(3 * time.Hour)); probe = probe.Add(time.Hour) {
		_, offset := probe.In(location).Zone()
		candidate := target.Add(-time.Duration(offset) * time.Second).In(location)
		if candidate.Year() == year && candidate.Month() == month && candidate.Day() == date && candidate.Hour()*60+candidate.Minute() == minute {
			if boundary.IsZero() || (last && candidate.After(boundary)) || (!last && candidate.Before(boundary)) {
				boundary = candidate
			}
		}
	}
	if !boundary.IsZero() {
		return boundary
	}
	// A skipped wall-clock time ends at the first actual minute after the gap.
	for probe := dayStart; probe.Before(nextStart); probe = probe.Add(time.Minute) {
		local := probe.In(location)
		if local.Year() == year && local.Month() == month && local.Day() == date && local.Hour()*60+local.Minute() >= minute {
			return probe
		}
	}
	return nextStart
}

func (c *Config) resolveDeviceGivenName(name string) string {
	if givenName, ok := c.NameMap[name]; ok {
		return givenName
	}
	return name
}

type DiskStats struct {
	Name           string
	GivenName      string
	IdleTime       time.Duration
	CommandType    string
	PowerCondition uint8
	Reads          uint64
	Writes         uint64
	SpinDownAt     time.Time
	SpinUpAt       time.Time
	LastIoAt       time.Time
	LastSpunDownAt time.Time
	SpunDown       bool
}

var previousSnapshots []DiskStats
var now = time.Now()
var lastNow = time.Now()
var issueSpindown = spindownDisk

func ObserveDiskActivity(config *Config) {
	actualSnapshot := diskstats.Snapshot()

	now = time.Now()
	resolveSymlinks(config)
	for _, stats := range actualSnapshot {
		d := &DiskStats{
			Name:   stats.Name,
			Reads:  stats.Reads,
			Writes: stats.Writes,
		}
		updateState(*d, config)
	}
	lastNow = now
}

func resolveSymlinks(config *Config) {
	if config.Defaults.SymlinkPolicy == 0 {
		return
	}
	for i := range config.Devices {
		device := config.Devices[i]
		if len(device.Name) == 0 {
			realPath, err := io.RealPath(device.GivenName)
			if err == nil {
				config.Devices[i].Name = realPath
				logToFile(config.Defaults.LogFile,
					fmt.Sprintf("symlink %s resolved to %s", device.GivenName, realPath))
			}
			if err != nil && config.Defaults.Debug {
				fmt.Printf("Cannot resolve sysmlink %s\n", device.GivenName)
			}
		}
	}
}

func updateState(tmp DiskStats, config *Config) {
	dsi := previousDiskStatsIndex(tmp.Name)
	if dsi < 0 {
		previousSnapshots = append(previousSnapshots, initDevice(tmp, config))
		return
	}

	// Strip monotonic readings so time spent suspended counts toward the gap.
	if now.Round(0).Sub(lastNow.Round(0)) > config.SkewTime {
		/* we slept too long, assume a suspend event and disks may be spun up */
		/* reset spin status and timers */
		previousSnapshots[dsi].SpinUpAt = now
		previousSnapshots[dsi].LastIoAt = now
		previousSnapshots[dsi].SpunDown = false
		logSpinupAfterSleep(previousSnapshots[dsi].Name, config.Defaults.LogFile)
	}
	ds := previousSnapshots[dsi]
	if ds.Writes == tmp.Writes && ds.Reads == tmp.Reads {
		if !ds.SpunDown || config.Defaults.IgnoreSpinDownDetection {

			idleDuration := now.Sub(ds.LastIoAt)
			timeSinceLastSpunDown := now.Sub(ds.LastSpunDownAt)

			idleTime := ds.IdleTime
			if config.WakeWindow != nil && config.WakeWindow.contains(now) {
				idleTime = 0
				if config.WakeIdle != nil {
					idleTime = *config.WakeIdle
				}
			}

			if ds.IdleTime != 0 && idleTime != 0 && idleDuration > idleTime && timeSinceLastSpunDown > idleTime {
				if ds.SpunDown && config.Defaults.IgnoreSpinDownDetection {
					fmt.Printf("%s spindown (ignoring prior spin down state)\n",
						config.resolveDeviceGivenName(ds.Name))
				} else {
					fmt.Printf("%s spindown\n",
						config.resolveDeviceGivenName(ds.Name))
				}
				device := fmt.Sprintf("/dev/%s", ds.Name)
				if err := issueSpindown(device, ds.CommandType, ds.PowerCondition, config.Defaults.Debug); err != nil {
					fmt.Println(err.Error())
				}
				previousSnapshots[dsi].LastSpunDownAt = now
				previousSnapshots[dsi].SpinDownAt = now
				previousSnapshots[dsi].SpunDown = true
			}
		}

	} else {
		/* disk had some activity */
		if ds.SpunDown {
			/* disk was spun down, thus it has just spun up */
			fmt.Printf("%s spinup\n", config.resolveDeviceGivenName(ds.Name))
			logSpinup(ds, config.Defaults.LogFile, config.resolveDeviceGivenName(ds.Name))
			previousSnapshots[dsi].SpinUpAt = now
		}
		previousSnapshots[dsi].Reads = tmp.Reads
		previousSnapshots[dsi].Writes = tmp.Writes
		previousSnapshots[dsi].LastIoAt = now
		previousSnapshots[dsi].SpunDown = false
	}

	if config.Defaults.Debug {
		ds = previousSnapshots[dsi]
		idleDuration := now.Sub(ds.LastIoAt)
		fmt.Printf("disk=%s command=%s spunDown=%t "+
			"reads=%d writes=%d idleTime=%v idleDuration=%v "+
			"spindown=%s spinup=%s lastIO=%s lastSpunDown=%s \n",
			ds.Name, ds.CommandType, ds.SpunDown,
			ds.Reads, ds.Writes, ds.IdleTime.Seconds(), math.RoundToEven(idleDuration.Seconds()),
			ds.SpinDownAt.Format(dateFormat), ds.SpinUpAt.Format(dateFormat), ds.LastIoAt.Format(dateFormat),
			ds.LastSpunDownAt.Format(dateFormat))
	}
}

func previousDiskStatsIndex(diskName string) int {
	for i, stats := range previousSnapshots {
		if stats.Name == diskName {
			return i
		}
	}
	return -1
}

func initDevice(stats DiskStats, config *Config) DiskStats {
	idle := config.Defaults.Idle
	command := config.Defaults.CommandType
	powerCondition := config.Defaults.PowerCondition
	deviceConf := deviceConfig(stats.Name, config)
	if deviceConf != nil {
		idle = deviceConf.Idle
		command = deviceConf.CommandType
		powerCondition = deviceConf.PowerCondition
	}

	return DiskStats{
		Name:           stats.Name,
		LastIoAt:       time.Now(),
		SpinUpAt:       time.Now(),
		SpunDown:       false,
		Writes:         stats.Writes,
		Reads:          stats.Reads,
		IdleTime:       idle,
		CommandType:    command,
		PowerCondition: powerCondition,
	}
}

func deviceConfig(diskName string, config *Config) *DeviceConf {
	for _, device := range config.Devices {
		if device.Name == diskName {
			return &device
		}
	}
	return &DeviceConf{
		Name:           diskName,
		CommandType:    config.Defaults.CommandType,
		PowerCondition: config.Defaults.PowerCondition,
		Idle:           config.Defaults.Idle,
	}
}

func spindownDisk(device, command string, powerCondition uint8, debug bool) error {
	switch command {
	case SCSI:
		if err := sgio.StartStopScsiDevice(device, powerCondition); err != nil {
			return fmt.Errorf("cannot spindown scsi disk %s:\n%s\n", device, err.Error())
		}
		return nil
	case ATA:
		if err := sgio.StopAtaDevice(device, debug); err != nil {
			return fmt.Errorf("cannot spindown ata disk %s:\n%s\n", device, err.Error())
		}
		return nil
	}
	return nil
}

func logSpinup(ds DiskStats, file, givenName string) {
	now := time.Now()
	text := fmt.Sprintf("date: %s, time: %s, disk: %s, running: %d, stopped: %d",
		now.Format("2006-01-02"), now.Format("15:04:05"), givenName,
		int(ds.SpinDownAt.Sub(ds.SpinUpAt).Seconds()), int(now.Sub(ds.SpinDownAt).Seconds()))
	logToFile(file, text)
}

func logSpinupAfterSleep(name, file string) {
	text := fmt.Sprintf("date: %s, time: %s, disk: %s, assuming disk spun up after long sleep",
		now.Format("2006-01-02"), now.Format("15:04:05"), name)
	logToFile(file, text)
}

func logToFile(file, text string) {
	if len(file) == 0 {
		return
	}

	cacheFile, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		log.Fatalf("Cannot open file %s. Error: %s", file, err)
	}
	if _, err = cacheFile.WriteString(text + "\n"); err != nil {
		log.Fatalf("Cannot write into file %s. Error: %s", file, err)
	}
	err = cacheFile.Close()
	if err != nil {
		log.Fatalf("Cannot close file %s. Error: %s", file, err)
	}
}

func (c *Config) String() string {
	var devices string
	for _, device := range c.Devices {
		devices += "{" + device.String() + "}"
	}
	window := "none"
	if c.WakeWindow != nil {
		window = fmt.Sprintf("%02d:%02d-%02d:%02d", c.WakeWindow.StartMinute/60, c.WakeWindow.StartMinute%60, c.WakeWindow.EndMinute/60, c.WakeWindow.EndMinute%60)
	}
	wakeIdle := "none"
	if c.WakeIdle != nil {
		wakeIdle = fmt.Sprintf("%v", c.WakeIdle.Seconds())
	}
	return fmt.Sprintf("symlinkPolicy=%d, defaultIdle=%v, defaultCommand=%s, defaultPowerCondition=%v, debug=%t, logFile=%s, devices=%s, ignoreSpinDownDetection=%t, wakeWindow=%s, wakeIdle=%s",
		c.Defaults.SymlinkPolicy, c.Defaults.Idle.Seconds(), c.Defaults.CommandType, c.Defaults.PowerCondition, c.Defaults.Debug, c.Defaults.LogFile, devices, c.Defaults.IgnoreSpinDownDetection, window, wakeIdle)
}

func (dc *DeviceConf) String() string {
	return fmt.Sprintf("name=%s, givenName=%s, idle=%v, commandType=%s, powerCondition=%v",
		dc.Name, dc.GivenName, dc.Idle.Seconds(), dc.CommandType, dc.PowerCondition)
}
