package main

import (
	"sort"
	"strings"
)

// rankEntry is one line of a ranking: all processes sharing a name.
type rankEntry struct {
	Name      string  `json:"name"`
	Value     float64 `json:"value"`
	Instances int     `json:"instances"`
}

// ranking is a top N plus the sum of everything else.
type ranking struct {
	Unit  string      `json:"unit"`
	Total float64     `json:"total"`
	Top   []rankEntry `json:"top"`
	Other float64     `json:"other"`
}

// rankCPU ranks process names by CPU usage between two snapshots, in percent
// of the whole machine (100 % = every logical CPU fully busy).
func rankCPU(prev, last *snapshot, n int) ranking {
	r := ranking{Unit: "%"}

	wall := last.at.Sub(prev.at)
	ncpu := last.ncpu
	if ncpu < 1 {
		ncpu = 1
	}

	if wall <= 0 {
		return r
	}

	capacity := float64(wall) * float64(ncpu)
	groups := map[string]*rankEntry{}

	for pid, cur := range last.procs {
		old, ok := prev.procs[pid]
		// A PID reused by another program between the two snapshots is skipped.
		if !ok || old.name != cur.name {
			continue
		}

		delta := cur.cpuTime - old.cpuTime
		if delta <= 0 {
			continue
		}

		addToGroup(groups, cur.name, float64(delta)/capacity*100)
	}

	return finishRanking(r, groups, n)
}

// rankMem ranks process names by memory, in megabytes.
func rankMem(last *snapshot, n int) ranking {
	r := ranking{Unit: "MB"}
	groups := map[string]*rankEntry{}

	for _, p := range last.procs {
		if p.memBytes == 0 {
			continue
		}

		addToGroup(groups, p.name, float64(p.memBytes)/(1024*1024))
	}

	return finishRanking(r, groups, n)
}

func addToGroup(groups map[string]*rankEntry, name string, value float64) {
	key := strings.ToLower(name)

	e, ok := groups[key]
	if !ok {
		e = &rankEntry{Name: name}
		groups[key] = e
	}

	e.Value += value
	e.Instances++
}

func finishRanking(r ranking, groups map[string]*rankEntry, n int) ranking {
	entries := make([]rankEntry, 0, len(groups))
	for _, e := range groups {
		entries = append(entries, *e)
		r.Total += e.Value
	}

	// Highest first; ties by name so the output is stable.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Value != entries[j].Value {
			return entries[i].Value > entries[j].Value
		}

		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	if len(entries) > n {
		for _, e := range entries[n:] {
			r.Other += e.Value
		}

		entries = entries[:n]
	}

	r.Top = entries

	return r
}
