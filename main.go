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
	"github.com/adelolmo/hd-idle/io"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultIdleTime     = 600 * time.Second
	symlinkResolveOnce  = 0
	symlinkResolveRetry = 1
)

func main() {

	if os.Getenv("START_HD_IDLE") == "false" {
		fmt.Println("START_HD_IDLE=false exiting now.")
		os.Exit(0)
	}

	singleDiskMode := false
	var disk string
	defaultConf := DefaultConf{
		Idle:           defaultIdleTime,
		CommandType:    SCSI,
		PowerCondition: 0,
		Debug:          false,
		SymlinkPolicy:  0,
	}
	var config = &Config{
		Devices:  []DeviceConf{},
		Defaults: defaultConf,
		NameMap:  map[string]string{},
	}
	var deviceConf *DeviceConf

	if len(os.Args) == 0 {
		usage()
		os.Exit(1)
	}

	for index, arg := range os.Args[1:] {
		switch arg {
		case "-t":
			var err error
			disk, err = argument(index)
			if err != nil {
				fmt.Println("Missing disk argument after -t. Must be a device (e.g. -t sda).")
				os.Exit(1)
			}
			singleDiskMode = true

		case "-s":
			s, err := argument(index)
			if err != nil {
				fmt.Println("Missing symlink_policy. Must be 0 or 1.")
				os.Exit(1)
			}
			switch s {
			case "0":
				config.Defaults.SymlinkPolicy = symlinkResolveOnce
			case "1":
				config.Defaults.SymlinkPolicy = symlinkResolveRetry
			default:
				fmt.Printf("Wrong symlink_policy -s %s. Must be 0 or 1.\n", s)
				os.Exit(1)
			}

		case "-a":
			if deviceConf != nil {
				config.Devices = append(config.Devices, *deviceConf)
			}

			name, err := argument(index)
			if err != nil {
				fmt.Println("Missing disk argument after -a. Must be a device (e.g. -a sda).")
				os.Exit(1)
			}

			deviceRealPath, err := io.RealPath(name)
			if err != nil {
				deviceRealPath = ""
				fmt.Printf("Unable to resolve symlink: %s\n", name)
			}
			deviceConf = &DeviceConf{
				Name:           deviceRealPath,
				GivenName:      name,
				Idle:           config.Defaults.Idle,
				CommandType:    config.Defaults.CommandType,
				PowerCondition: config.Defaults.PowerCondition,
			}
			config.NameMap[deviceRealPath] = name

		case "-i":
			s, err := argument(index)
			if err != nil {
				fmt.Println("Missing idle_time after -i. Must be a number.")
				os.Exit(1)
			}
			idle, err := strconv.Atoi(s)
			if err != nil {
				fmt.Printf("Wrong idle_time -i %d. Must be a number.", idle)
				os.Exit(1)
			}
			if deviceConf == nil {
				config.Defaults.Idle = time.Duration(idle) * time.Second
				break
			}
			deviceConf.Idle = time.Duration(idle) * time.Second

		case "-I":
			config.Defaults.IgnoreSpinDownDetection = true

		case "-w":
			s, err := argument(index)
			if err != nil {
				fmt.Println("Missing wake window after -w. Must be HH:MM-HH:MM.")
				os.Exit(1)
			}
			window, err := parseWakeWindow(s)
			if err != nil {
				fmt.Printf("Invalid wake window -w %s. Must be HH:MM-HH:MM with different start and end times.\n", s)
				os.Exit(1)
			}
			config.WakeWindow = window

		case "-W":
			s, err := argument(index)
			if err != nil {
				fmt.Println("Missing wake idle time after -W. Must be a non-negative number of seconds.")
				os.Exit(1)
			}
			idle, err := parseWakeIdle(s)
			if err != nil {
				fmt.Printf("Invalid wake idle time -W %s. Must be a non-negative number of seconds.\n", s)
				os.Exit(1)
			}
			config.WakeIdle = &idle

		case "-c":
			command, err := argument(index)
			if err != nil {
				fmt.Println("Missing command_type after -c. Must be one of: scsi, ata.")
				os.Exit(1)
			}
			switch command {
			case SCSI, ATA:
				if deviceConf == nil {
					config.Defaults.CommandType = command
					break
				}
				deviceConf.CommandType = command
			default:
				fmt.Printf("Wrong command_type -c %s. Must be one of: scsi, ata.", command)
				os.Exit(1)
			}

		case "-p":
			s, err := argument(index)
			if err != nil {
				fmt.Println("Missing power condition after -p. Must be a number from 0-15.")
				os.Exit(1)
			}
			powerCondition, err := strconv.ParseUint(s, 0, 4)
			if err != nil {
				fmt.Printf("Invalid power condition %s: %s", s, err.Error())
				os.Exit(1)
			}
			if deviceConf == nil {
				config.Defaults.PowerCondition = uint8(powerCondition)
				break
			}
			deviceConf.PowerCondition = uint8(powerCondition)

		case "-l":
			logfile, err := argument(index)
			if err != nil {
				fmt.Println("Missing logfile after -l.")
				os.Exit(1)
			}
			config.Defaults.LogFile = logfile

		case "-d":
			config.Defaults.Debug = true

		case "-h":
			usage()
			os.Exit(0)
		}
	}
	if config.WakeIdle != nil && config.WakeWindow == nil {
		fmt.Println("-W requires a wake window set with -w.")
		os.Exit(1)
	}

	if singleDiskMode {
		if err := spindownDisk(
			disk,
			config.Defaults.CommandType,
			config.Defaults.PowerCondition,
			config.Defaults.Debug,
		); err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}
		os.Exit(0)
	}

	if deviceConf != nil {
		config.Devices = append(config.Devices, *deviceConf)
	}
	fmt.Println(config.String())

	interval := poolInterval(config)
	config.SkewTime = interval * 3
	for {
		ObserveDiskActivity(config)
		time.Sleep(interval)
	}
}

func argument(index int) (string, error) {
	argIndex := index + 2
	if argIndex >= len(os.Args) {
		return "", fmt.Errorf("option requires argument")
	}
	arg := os.Args[argIndex]
	if arg == "" || arg[:1] == "-" {
		return "", fmt.Errorf("option requires argument")
	}
	return arg, nil
}

func usage() {
	fmt.Println("usage: hd-idle [-t <disk>] [-s <symlink_policy>] [-a <name>] [-i <idle_time>] " +
		"[-c <command_type>] [-p power_condition] [-l <logfile>] [-w HH:MM-HH:MM] [-W <seconds>] [-d] [-I] [-h]")
}

func parseWakeIdle(value string) (time.Duration, error) {
	seconds, err := strconv.ParseUint(value, 10, 64)
	if err != nil || seconds > uint64((1<<63-1)/int64(time.Second)) {
		return 0, fmt.Errorf("invalid wake idle time")
	}
	return time.Duration(seconds) * time.Second, nil
}

func parseWakeWindow(value string) (*WakeWindow, error) {
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid wake window")
	}
	start, err := parseClockMinute(parts[0])
	if err != nil {
		return nil, err
	}
	end, err := parseClockMinute(parts[1])
	if err != nil || start == end {
		return nil, fmt.Errorf("invalid wake window")
	}
	return &WakeWindow{StartMinute: start, EndMinute: end}, nil
}

func parseClockMinute(value string) (int, error) {
	if len(value) != 5 || value[2] != ':' {
		return 0, fmt.Errorf("invalid clock time")
	}
	for _, index := range []int{0, 1, 3, 4} {
		if value[index] < '0' || value[index] > '9' {
			return 0, fmt.Errorf("invalid clock time")
		}
	}
	hour, _ := strconv.Atoi(value[:2])
	minute, _ := strconv.Atoi(value[3:])
	if hour > 23 || minute > 59 {
		return 0, fmt.Errorf("invalid clock time")
	}
	return hour*60 + minute, nil
}

func poolInterval(config *Config) time.Duration {
	interval := defaultIdleTime
	enabled := config.Defaults.Idle > 0
	if enabled && config.Defaults.Idle < interval {
		interval = config.Defaults.Idle
	}
	for _, dev := range config.Devices {
		if dev.Idle == 0 {
			continue
		}
		enabled = true
		if dev.Idle < interval {
			interval = dev.Idle
		}
	}
	if enabled && config.WakeIdle != nil && *config.WakeIdle > 0 && *config.WakeIdle < interval {
		interval = *config.WakeIdle
	}

	sleepTime := interval / 10
	if sleepTime == 0 {
		return time.Second
	}
	return sleepTime
}
