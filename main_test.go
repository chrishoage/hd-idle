package main

import (
	"testing"
	"time"
)

func TestIntervalWithZeroSecondsIdle(t *testing.T) {
	confs := []DeviceConf{{
		Name:        "test",
		GivenName:   "test",
		Idle:        0,
		CommandType: "ata",
	}}
	interval := poolInterval(&Config{Devices: confs, Defaults: DefaultConf{Idle: defaultIdleTime}})
	if interval != defaultIdleTime/10 {
		t.Fatalf("interval should be the default. it was %d", interval)
	}
}

func TestIntervalWith300SecondsIdle(t *testing.T) {
	confs := []DeviceConf{{
		Name:        "test",
		GivenName:   "test",
		Idle:        300 * time.Second,
		CommandType: "ata",
	}}
	interval := poolInterval(&Config{Devices: confs, Defaults: DefaultConf{Idle: defaultIdleTime}})
	if interval != 30*time.Second {
		t.Fatalf("interval should be the 30s. it was %v", interval)
	}
}

func TestIntervalWithWakeIdle(t *testing.T) {
	tests := []struct {
		name        string
		defaultIdle time.Duration
		devices     []DeviceConf
		wakeIdle    time.Duration
		want        time.Duration
	}{
		{"short wake idle without named disks", defaultIdleTime, nil, time.Second, 100 * time.Millisecond},
		{"short wake idle with named disk", defaultIdleTime, []DeviceConf{{Idle: 300 * time.Second}}, 10 * time.Second, time.Second},
		{"wake idle longer than nominal", defaultIdleTime, []DeviceConf{{Idle: 300 * time.Second}}, 3 * time.Hour, 30 * time.Second},
		{"zero wake idle suppresses spindown", defaultIdleTime, nil, 0, defaultIdleTime / 10},
		{"global idle disabled", 0, nil, time.Second, defaultIdleTime / 10},
		{"named disk enabled with global idle disabled", 0, []DeviceConf{{Idle: 300 * time.Second}}, time.Second, 100 * time.Millisecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := &Config{Devices: test.devices, Defaults: DefaultConf{Idle: test.defaultIdle}, WakeIdle: &test.wakeIdle}
			if got := poolInterval(config); got != test.want {
				t.Fatalf("Expected %v but found %v", test.want, got)
			}
		})
	}
}
