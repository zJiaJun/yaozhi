// SPDX-License-Identifier: GPL-3.0-only

package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatsDays(t *testing.T) {
	day1 := time.Date(2026, 11, 8, 23, 50, 0, 0, time.Local)
	day2 := day1.Add(20 * time.Minute)

	var s Stats
	s = s.add(day1, 3*time.Second, 400*time.Millisecond)
	s = s.add(day1, 1*time.Second, 500*time.Millisecond)
	if s.Today != 2 || s.AvgAudio() != 2*time.Second || s.AvgLatency() != 450*time.Millisecond || s.Total != 2 {
		t.Fatalf("day 1: %+v", s)
	}
	if got := s.on(day2); got.Today != 0 || got.AvgAudio() != 0 || got.Total != 2 || got.Day != "2026-11-09" {
		t.Fatalf("a new day keeps only the total: %+v", got)
	}
	s = s.add(day2, 2*time.Second, 300*time.Millisecond)
	if s.Today != 1 || s.Total != 3 {
		t.Fatalf("day 2: %+v", s)
	}
}

func TestStatsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "stats.json")
	if s := loadStats(path); s != (Stats{}) {
		t.Fatalf("missing file: %+v", s)
	}
	want := Stats{}.add(time.Now(), time.Second, 400*time.Millisecond)
	if err := saveStats(path, want); err != nil {
		t.Fatal(err)
	}
	if got := loadStats(path); got != want {
		t.Fatalf("read back %+v, want %+v", got, want)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := loadStats(path); s != (Stats{}) {
		t.Fatalf("broken file: %+v", s)
	}
}
