package main

import (
	"errors"
	"sync"
	"time"
)

// procSample is the state of one process at the time of a snapshot.
type procSample struct {
	name     string
	cpuTime  time.Duration // cumulative user + kernel time
	memBytes uint64        // private working set (Windows) or resident set size (Linux)
}

// snapshot is the list of processes at a given time, keyed by PID.
type snapshot struct {
	at    time.Time
	ncpu  int
	procs map[int]procSample
}

func (s *snapshot) names() map[int]string {
	names := make(map[int]string, len(s.procs))
	for pid, p := range s.procs {
		names[pid] = p.name
	}

	return names
}

const (
	// maxSnapshotAge is how long a snapshot is reused by keys that only need
	// the current state (memory, PID-to-name for the network keys).
	maxSnapshotAge = 30 * time.Second
	// minCPUWindow is the shortest interval used to compute CPU usage.
	minCPUWindow = time.Second
)

var errNoSnapshot = errors.New("process list unavailable")

// sampler shares process snapshots between item keys: the agent may poll
// several keys at the same time, and each snapshot walks every process.
type sampler struct {
	mu    sync.Mutex
	take  func() (*snapshot, error)
	now   func() time.Time
	sleep func(time.Duration)

	prev, last *snapshot
}

func newSampler(take func() (*snapshot, error)) *sampler {
	return &sampler{take: take, now: time.Now, sleep: time.Sleep}
}

// fresh returns a snapshot taken less than maxSnapshotAge ago.
func (s *sampler) fresh() (*snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.last != nil && s.now().Sub(s.last.at) < maxSnapshotAge {
		return s.last, nil
	}

	return s.shift()
}

// pair returns two snapshots at least minCPUWindow apart, the latest being
// fresh, to compute CPU usage over that window.
func (s *sampler) pair() (*snapshot, *snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()

	switch {
	case s.last == nil || now.Sub(s.last.at) >= maxSnapshotAge:
		// Nothing recent: measure over a fresh one-second window.
		if _, err := s.shift(); err != nil {
			return nil, nil, err
		}

		s.sleep(minCPUWindow)

		if _, err := s.shift(); err != nil {
			return nil, nil, err
		}
	case now.Sub(s.last.at) >= minCPUWindow:
		// Recent snapshot: measure from it to now.
		if _, err := s.shift(); err != nil {
			return nil, nil, err
		}
	case s.prev == nil || s.last.at.Sub(s.prev.at) < minCPUWindow:
		// Latest snapshot too recent and no usable window: wait and measure.
		s.sleep(minCPUWindow - now.Sub(s.last.at))

		if _, err := s.shift(); err != nil {
			return nil, nil, err
		}
	}

	return s.prev, s.last, nil
}

// shift takes a new snapshot and keeps the previous one.
func (s *sampler) shift() (*snapshot, error) {
	snap, err := s.take()
	if err != nil {
		return nil, err
	}

	if snap == nil {
		return nil, errNoSnapshot
	}

	s.prev, s.last = s.last, snap

	return snap, nil
}
