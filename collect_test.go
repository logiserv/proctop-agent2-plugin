package main

import (
	"strings"
	"testing"
	"time"
)

func snap(at time.Time, ncpu int, procs map[int]procSample) *snapshot {
	return &snapshot{at: at, ncpu: ncpu, procs: procs}
}

func TestRankCPU(t *testing.T) {
	t0 := time.Unix(1000, 0)
	prev := snap(t0, 4, map[int]procSample{
		1: {name: "mcshield.exe", cpuTime: 10 * time.Second},
		2: {name: "chrome.exe", cpuTime: 1 * time.Second},
		3: {name: "Chrome.exe", cpuTime: 1 * time.Second},
		4: {name: "old.exe", cpuTime: 5 * time.Second},
		5: {name: "idle.exe", cpuTime: 3 * time.Second},
	})
	// One second later on 4 CPUs: 4 s of CPU capacity.
	last := snap(t0.Add(time.Second), 4, map[int]procSample{
		1: {name: "mcshield.exe", cpuTime: 13 * time.Second},      // 3 s = 75 %
		2: {name: "chrome.exe", cpuTime: 1200 * time.Millisecond}, // 0.2 s
		3: {name: "Chrome.exe", cpuTime: 1200 * time.Millisecond}, // 0.2 s, same group
		4: {name: "reused.exe", cpuTime: 9 * time.Second},         // PID reused: skipped
		5: {name: "idle.exe", cpuTime: 3 * time.Second},           // no CPU: skipped
		6: {name: "new.exe", cpuTime: 50 * time.Second},           // not in prev: skipped
	})

	r := rankCPU(prev, last, 1)

	if len(r.Top) != 1 || r.Top[0].Name != "mcshield.exe" {
		t.Fatalf("top = %+v, want mcshield.exe first", r.Top)
	}

	if got := r.Top[0].Value; got < 74.99 || got > 75.01 {
		t.Errorf("mcshield.exe = %.2f %%, want 75 %%", got)
	}

	if got := r.Other; got < 9.99 || got > 10.01 {
		t.Errorf("other = %.2f %%, want 10 %% (two chrome.exe at 5 %% each)", got)
	}

	full := rankCPU(prev, last, 10)
	if len(full.Top) != 2 || full.Top[1].Instances != 2 {
		t.Errorf("chrome.exe should be grouped across PIDs and case: %+v", full.Top)
	}
}

func TestRankMem(t *testing.T) {
	last := snap(time.Unix(0, 0), 1, map[int]procSample{
		1: {name: "sqlservr.exe", memBytes: 2048 << 20},
		2: {name: "svchost.exe", memBytes: 100 << 20},
		3: {name: "svchost.exe", memBytes: 50 << 20},
		4: {name: "kworker/0:1", memBytes: 0},
	})

	r := rankMem(last, 10)

	if len(r.Top) != 2 {
		t.Fatalf("top = %+v, want 2 entries (zero memory skipped)", r.Top)
	}

	if r.Top[0].Name != "sqlservr.exe" || r.Top[1].Value != 150 || r.Top[1].Instances != 2 {
		t.Errorf("unexpected ranking: %+v", r.Top)
	}
}

func TestSamplerPair(t *testing.T) {
	now := time.Unix(1000, 0)
	calls := 0

	s := newSampler(func() (*snapshot, error) {
		calls++

		return snap(now, 1, map[int]procSample{}), nil
	})
	s.now = func() time.Time { return now }
	s.sleep = func(d time.Duration) { now = now.Add(d) }

	// Cold start: two snapshots one second apart.
	prev, last, err := s.pair()
	if err != nil {
		t.Fatal(err)
	}

	if calls != 2 || last.at.Sub(prev.at) != time.Second {
		t.Fatalf("cold start: %d calls, window %s", calls, last.at.Sub(prev.at))
	}

	// 10 s later: one new snapshot, window measured from the last one.
	now = now.Add(10 * time.Second)

	prev, last, _ = s.pair()
	if calls != 3 || last.at.Sub(prev.at) != 10*time.Second {
		t.Fatalf("warm: %d calls, window %s", calls, last.at.Sub(prev.at))
	}

	// Immediately after: the existing window is reused.
	prev, last, _ = s.pair()
	if calls != 3 || last.at.Sub(prev.at) != 10*time.Second {
		t.Fatalf("reuse: %d calls, window %s", calls, last.at.Sub(prev.at))
	}

	// fresh() reuses the last snapshot while it is recent.
	if _, err := s.fresh(); err != nil || calls != 3 {
		t.Fatalf("fresh should reuse the snapshot: %d calls", calls)
	}

	// Stale (> 30 s): new one-second window.
	now = now.Add(time.Minute)

	prev, last, _ = s.pair()
	if calls != 5 || last.at.Sub(prev.at) != time.Second {
		t.Fatalf("stale: %d calls, window %s", calls, last.at.Sub(prev.at))
	}
}

func TestFormatRankingText(t *testing.T) {
	r := ranking{
		Unit:  "%",
		Top:   []rankEntry{{Name: "mcshield.exe", Value: 79.09, Instances: 1}, {Name: "chrome.exe", Value: 0.29, Instances: 12}},
		Other: 1.05,
	}

	got := formatRanking(r, formatText)
	want := strings.Join([]string{
		"Process             CPU",
		"mcshield.exe     79.09%",
		"chrome.exe (12)   0.29%",
		"Other processes   1.05%",
	}, "\n")

	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	if strings.ContainsAny(got, "{}") {
		t.Error("text output must not contain curly braces")
	}

	if formatRanking(ranking{Unit: "%"}, formatText) != "" {
		t.Error("an empty ranking should give an empty string")
	}
}

func TestFormatValueMemory(t *testing.T) {
	if got := formatValue(350, "MB"); got != "350 MB" {
		t.Errorf("got %q", got)
	}

	if got := formatValue(2048, "MB"); got != "2.0 GB" {
		t.Errorf("got %q", got)
	}
}
