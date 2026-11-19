//go:build linux

package main

import (
	"os"
	"runtime"
	"strconv"
	"time"
)

// takeSnapshot reads /proc/[pid]/stat and /proc/[pid]/statm, which every user
// can read: the agent does not need to run as root.
func takeSnapshot() (*snapshot, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}

	snap := &snapshot{at: time.Now(), ncpu: linuxCPUCount(), procs: map[int]procSample{}}
	pageSize := uint64(os.Getpagesize())

	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || !e.IsDir() {
			continue
		}

		dir := "/proc/" + e.Name()

		// A process may exit while we read it: skip it silently.
		stat, err := os.ReadFile(dir + "/stat")
		if err != nil {
			continue
		}

		name, cpu, err := parseProcStat(string(stat))
		if err != nil {
			continue
		}

		var mem uint64
		if statm, err := os.ReadFile(dir + "/statm"); err == nil {
			if pages, err := parseStatmRSS(string(statm)); err == nil {
				mem = pages * pageSize
			}
		}

		snap.procs[pid] = procSample{name: name, cpuTime: cpu, memBytes: mem}
	}

	return snap, nil
}

func linuxCPUCount() int {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return runtime.NumCPU()
	}
	defer f.Close()

	if n := countCPUs(f); n > 0 {
		return n
	}

	return runtime.NumCPU()
}
