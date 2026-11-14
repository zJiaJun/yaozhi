// SPDX-License-Identifier: GPL-3.0-only

package app

import "time"

// Stats counts finished streams, for the numbers of the main window. It
// holds counts and durations only, never audio or text, and is kept in a
// small JSON file so that "today" survives a restart.
type Stats struct {
	Day     string        `json:"day"`        // the local date Today* count, as 2006-01-02
	Today   int           `json:"today"`      // streams finished on Day
	Audio   time.Duration `json:"audio_ns"`   // their speech, summed
	Latency time.Duration `json:"latency_ns"` // from release to the input method told to finish, summed
	Total   int           `json:"total"`      // streams finished ever
}

// AvgAudio is the mean speech of today's streams.
func (s Stats) AvgAudio() time.Duration {
	if s.Today == 0 {
		return 0
	}
	return s.Audio / time.Duration(s.Today)
}

// AvgLatency is the mean time from release to the input method told to
// finish, today.
func (s Stats) AvgLatency() time.Duration {
	if s.Today == 0 {
		return 0
	}
	return s.Latency / time.Duration(s.Today)
}

// on returns s as of now: today's counts start over on a new day.
func (s Stats) on(now time.Time) Stats {
	if day := now.Format(time.DateOnly); s.Day != day {
		s.Day, s.Today, s.Audio, s.Latency = day, 0, 0, 0
	}
	return s
}

// add counts a finished stream.
func (s Stats) add(now time.Time, audio, latency time.Duration) Stats {
	s = s.on(now)
	s.Today++
	s.Audio += audio
	s.Latency += latency
	s.Total++
	return s
}

// loadStats reads the file; a missing or broken one is a fresh start.
func loadStats(path string) Stats {
	var s Stats
	if !loadJSON(path, &s) {
		s = Stats{}
	}
	return s
}

// saveStats writes the file.
func saveStats(path string, s Stats) error { return saveJSON(path, s) }
