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
	"os"
	"testing"
	"time"
)

func TestEmptyWakeWindowArgument(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"hd-idle", "-w", ""}
	if _, err := argument(0); err == nil {
		t.Fatal("Expected an error for an empty -w argument")
	}
}

func TestParseWakeWindow(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  *WakeWindow
	}{
		{"morning", "00:00-09:00", &WakeWindow{0, 540}},
		{"crosses midnight", "22:30-06:15", &WakeWindow{1350, 375}},
		{"same times", "09:00-09:00", nil},
		{"short format", "0:00-09:00", nil},
		{"invalid hour", "24:00-09:00", nil},
		{"invalid minute", "00:60-09:00", nil},
		{"extra separator", "00:00-09:00-10:00", nil},
		{"non-digit", "aa:00-09:00", nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseWakeWindow(test.value)
			if test.want == nil {
				if err == nil {
					t.Fatalf("Expected an error for %q", test.value)
				}
				return
			}
			if err != nil || got == nil || *got != *test.want {
				t.Fatalf("Expected %v but found %v, error %v", test.want, got, err)
			}
		})
	}
}

func TestWakeWindowBoundaries(t *testing.T) {
	location := time.FixedZone("local", 2*60*60)
	tests := []struct {
		name   string
		window WakeWindow
		at     time.Time
		want   bool
	}{
		{"at start", WakeWindow{0, 540}, time.Date(2026, 9, 25, 0, 0, 0, 0, location), true},
		{"before end", WakeWindow{0, 540}, time.Date(2026, 9, 25, 8, 59, 59, 0, location), true},
		{"at end", WakeWindow{0, 540}, time.Date(2026, 9, 25, 9, 0, 0, 0, location), false},
		{"overnight start", WakeWindow{1320, 360}, time.Date(2026, 9, 24, 22, 0, 0, 0, location), true},
		{"overnight midnight", WakeWindow{1320, 360}, time.Date(2026, 9, 25, 0, 0, 0, 0, location), true},
		{"overnight end", WakeWindow{1320, 360}, time.Date(2026, 9, 25, 6, 0, 0, 0, location), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.window.contains(test.at); got != test.want {
				t.Fatalf("Expected %t but found %t", test.want, got)
			}
		})
	}
}

func TestWakeWindowDSTBoundaries(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	fallback := &WakeWindow{0, 90}
	firstEnd := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC).In(location)
	secondEnd := firstEnd.Add(time.Hour)
	if !fallback.contains(firstEnd.Add(15*time.Minute)) || fallback.contains(secondEnd) {
		t.Fatal("Expected the repeated end hour to remain protected until its final occurrence")
	}
	spring := &WakeWindow{0, 150}
	firstAfterGap := time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC).In(location)
	if !spring.contains(firstAfterGap.Add(-time.Minute)) || spring.contains(firstAfterGap) {
		t.Fatal("Expected a missing end time to end at the first actual instant after the gap")
	}
}

func TestWakeWindowIdleTimeout(t *testing.T) {
	oldNow, oldLastNow, oldSnapshots := now, lastNow, previousSnapshots
	oldIssueSpindown := issueSpindown
	t.Cleanup(func() {
		now, lastNow, previousSnapshots = oldNow, oldLastNow, oldSnapshots
		issueSpindown = oldIssueSpindown
	})
	spindownCalls := 0
	issueSpindown = func(device, command string, powerCondition uint8, debug bool) error {
		spindownCalls++
		return nil
	}
	location := time.FixedZone("local", 0)
	start := time.Date(2026, 9, 25, 0, 0, 0, 0, location)
	wakeIdle := 3 * time.Hour
	config := &Config{SkewTime: 5 * time.Minute, WakeWindow: &WakeWindow{0, 540}, WakeIdle: &wakeIdle}
	step := func(at time.Time) {
		now = at
		lastNow = at.Add(-time.Minute)
		updateState(DiskStats{Name: "test"}, config)
	}
	previousSnapshots = []DiskStats{{Name: "test", IdleTime: 30 * time.Minute, LastIoAt: start.Add(-time.Hour)}}
	step(start.Add(2 * time.Hour))
	if spindownCalls != 0 {
		t.Fatal("Expected pre-window idle time to count toward the three-hour timeout")
	}
	step(start.Add(2*time.Hour + time.Minute))
	if spindownCalls != 1 {
		t.Fatalf("Expected spindown inside the window after three hours idle, found %d", spindownCalls)
	}
	previousSnapshots = []DiskStats{{Name: "test", IdleTime: 30 * time.Minute, LastIoAt: start.Add(8 * time.Hour)}}
	step(start.Add(9 * time.Hour))
	if spindownCalls != 2 || !previousSnapshots[0].LastIoAt.Equal(start.Add(8*time.Hour)) {
		t.Fatalf("Expected immediate spindown at window exit using actual last I/O, found %+v and %d calls", previousSnapshots[0], spindownCalls)
	}
	previousSnapshots = []DiskStats{{Name: "test", IdleTime: 30 * time.Minute, LastIoAt: start.Add(8*time.Hour + 45*time.Minute)}}
	step(start.Add(9 * time.Hour))
	if spindownCalls != 2 {
		t.Fatal("Expected recent activity to delay spindown after window exit")
	}
	step(start.Add(9*time.Hour + 16*time.Minute))
	if spindownCalls != 3 {
		t.Fatalf("Expected spindown after nominal timeout from actual activity, found %d", spindownCalls)
	}
	previousSnapshots = []DiskStats{{Name: "test", IdleTime: 0, LastIoAt: start.Add(20 * time.Hour)}}
	step(start.Add(29 * time.Hour))
	if spindownCalls != 3 {
		t.Fatal("Expected -i 0 to disable spindown even with a wake idle timeout")
	}
}

func TestWakeWindowSuppressesSpindownWithIgnoreDetection(t *testing.T) {
	oldNow, oldLastNow, oldSnapshots := now, lastNow, previousSnapshots
	oldIssueSpindown := issueSpindown
	t.Cleanup(func() {
		now, lastNow, previousSnapshots = oldNow, oldLastNow, oldSnapshots
		issueSpindown = oldIssueSpindown
	})
	spindownCalls := 0
	issueSpindown = func(device, command string, powerCondition uint8, debug bool) error {
		spindownCalls++
		return nil
	}
	location := time.FixedZone("local", 2*60*60)
	now = time.Date(2026, 9, 25, 8, 0, 0, 0, location)
	lastNow = now.Add(-time.Minute)
	previousSnapshots = []DiskStats{{Name: "test", IdleTime: time.Minute, LastIoAt: now.Add(-time.Hour), LastSpunDownAt: now.Add(-time.Hour), SpunDown: true}}
	config := &Config{Defaults: DefaultConf{IgnoreSpinDownDetection: true}, SkewTime: 5 * time.Minute, WakeWindow: &WakeWindow{0, 540}}
	updateState(DiskStats{Name: "test"}, config)
	if previousSnapshots[0].SpinDownAt != (time.Time{}) || previousSnapshots[0].LastSpunDownAt != now.Add(-time.Hour) || !previousSnapshots[0].SpunDown || spindownCalls != 0 {
		t.Fatalf("Expected no spindown during the wake window, found %+v", previousSnapshots[0])
	}
	wakeIdle := time.Duration(0)
	config.WakeIdle = &wakeIdle
	now = now.Add(time.Minute)
	lastNow = now.Add(-time.Minute)
	updateState(DiskStats{Name: "test"}, config)
	if spindownCalls != 0 {
		t.Fatal("Expected -W 0 to suppress spindown even with -I")
	}
}

func TestWakeWindowWithoutWakeIdleSuppressesThenUsesNominalTimeout(t *testing.T) {
	oldNow, oldLastNow, oldSnapshots := now, lastNow, previousSnapshots
	oldIssueSpindown := issueSpindown
	t.Cleanup(func() {
		now, lastNow, previousSnapshots = oldNow, oldLastNow, oldSnapshots
		issueSpindown = oldIssueSpindown
	})
	calls := 0
	issueSpindown = func(device, command string, powerCondition uint8, debug bool) error {
		calls++
		return nil
	}
	location := time.FixedZone("local", 0)
	end := time.Date(2026, 9, 25, 9, 0, 0, 0, location)
	now = end.Add(-time.Minute)
	lastNow = now.Add(-time.Minute)
	lastIo := end.Add(-time.Hour)
	previousSnapshots = []DiskStats{{Name: "test", IdleTime: 30 * time.Minute, LastIoAt: lastIo}}
	config := &Config{SkewTime: 5 * time.Minute, WakeWindow: &WakeWindow{0, 540}}
	updateState(DiskStats{Name: "test"}, config)
	if calls != 0 || !previousSnapshots[0].LastIoAt.Equal(lastIo) {
		t.Fatalf("Expected suppression without changing last I/O, found %+v and %d calls", previousSnapshots[0], calls)
	}
	now = end
	lastNow = now.Add(-time.Minute)
	updateState(DiskStats{Name: "test"}, config)
	if calls != 1 || !previousSnapshots[0].LastIoAt.Equal(lastIo) {
		t.Fatalf("Expected spindown at window exit using nominal timeout, found %+v and %d calls", previousSnapshots[0], calls)
	}
}

func TestWakeWindowIgnoreDetectionUsesWakeIdleForRepeat(t *testing.T) {
	oldNow, oldLastNow, oldSnapshots := now, lastNow, previousSnapshots
	oldIssueSpindown := issueSpindown
	t.Cleanup(func() {
		now, lastNow, previousSnapshots = oldNow, oldLastNow, oldSnapshots
		issueSpindown = oldIssueSpindown
	})
	calls := 0
	issueSpindown = func(device, command string, powerCondition uint8, debug bool) error {
		calls++
		return nil
	}
	location := time.FixedZone("local", 0)
	wakeIdle := 3 * time.Hour
	config := &Config{Defaults: DefaultConf{IgnoreSpinDownDetection: true}, SkewTime: 5 * time.Minute, WakeWindow: &WakeWindow{0, 540}, WakeIdle: &wakeIdle}
	now = time.Date(2026, 9, 25, 5, 0, 0, 0, location)
	lastNow = now.Add(-time.Minute)
	previousSnapshots = []DiskStats{{Name: "test", IdleTime: 30 * time.Minute, LastIoAt: now.Add(-5 * time.Hour), LastSpunDownAt: now.Add(-time.Hour), SpunDown: true}}
	updateState(DiskStats{Name: "test"}, config)
	if calls != 0 {
		t.Fatal("Expected the three-hour repeat guard inside the window")
	}
	now = now.Add(2*time.Hour + time.Minute)
	lastNow = now.Add(-time.Minute)
	updateState(DiskStats{Name: "test"}, config)
	if calls != 1 {
		t.Fatalf("Expected one repeat after three hours, found %d", calls)
	}
}

func TestParseWakeIdle(t *testing.T) {
	tests := []struct {
		value string
		want  time.Duration
		valid bool
	}{
		{"0", 0, true},
		{"10800", 3 * time.Hour, true},
		{"-1", 0, false},
		{"1.5", 0, false},
		{"9223372037", 0, false},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			got, err := parseWakeIdle(test.value)
			if (err == nil) != test.valid || (test.valid && got != test.want) {
				t.Fatalf("Expected %v and valid=%t but found %v and error %v", test.want, test.valid, got, err)
			}
		})
	}
}

func TestShortWakeIdleAcrossSecondBoundary(t *testing.T) {
	oldNow, oldLastNow, oldSnapshots := now, lastNow, previousSnapshots
	oldIssueSpindown := issueSpindown
	t.Cleanup(func() {
		now, lastNow, previousSnapshots = oldNow, oldLastNow, oldSnapshots
		issueSpindown = oldIssueSpindown
	})
	calls := 0
	issueSpindown = func(device, command string, powerCondition uint8, debug bool) error {
		calls++
		return nil
	}
	location := time.FixedZone("local", 0)
	base := time.Date(2026, 9, 25, 1, 0, 0, 900000000, location)
	wakeIdle := time.Second
	config := &Config{SkewTime: 300 * time.Millisecond, WakeWindow: &WakeWindow{0, 540}, WakeIdle: &wakeIdle}
	previousSnapshots = []DiskStats{{Name: "test", IdleTime: 30 * time.Minute, LastIoAt: base}}
	for i := 1; i <= 11; i++ {
		lastNow = base.Add(time.Duration(i-1) * 100 * time.Millisecond)
		now = base.Add(time.Duration(i) * 100 * time.Millisecond)
		updateState(DiskStats{Name: "test"}, config)
		if !previousSnapshots[0].LastIoAt.Equal(base) {
			t.Fatalf("Expected no idle reset after a normal 100ms poll, found %v", previousSnapshots[0].LastIoAt)
		}
		if i <= 10 && calls != 0 {
			t.Fatalf("Expected no spindown at or before one second idle, found %d calls", calls)
		}
	}
	if calls != 1 {
		t.Fatalf("Expected one spindown after one second idle, found %d", calls)
	}
	lastNow = now
	now = now.Add(time.Minute)
	updateState(DiskStats{Name: "test"}, config)
	if !previousSnapshots[0].LastIoAt.Equal(now) || previousSnapshots[0].SpunDown {
		t.Fatalf("Expected suspended-system poll to reset idle state, found %+v", previousSnapshots[0])
	}
}
