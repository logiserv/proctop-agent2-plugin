//go:build windows

package main

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// takeSnapshot reads every process with a single NtQuerySystemInformation
// call. Unlike OpenProcess per PID, this also works for protected processes
// (antivirus, system services).
func takeSnapshot() (*snapshot, error) {
	buf, err := querySystemProcessInformation()
	if err != nil {
		return nil, err
	}

	snap := &snapshot{
		at:    time.Now(),
		ncpu:  int(windows.GetActiveProcessorCount(windows.ALL_PROCESSOR_GROUPS)),
		procs: map[int]procSample{},
	}

	for offset := uintptr(0); ; {
		if offset+unsafe.Sizeof(windows.SYSTEM_PROCESS_INFORMATION{}) > uintptr(len(buf)) {
			break
		}

		info := (*windows.SYSTEM_PROCESS_INFORMATION)(unsafe.Pointer(&buf[offset]))
		pid := int(info.UniqueProcessID)

		// PID 0 is the idle process: its "CPU time" is the idle time.
		if pid != 0 {
			name := info.ImageName.String()
			if name == "" {
				name = "System"
			}

			snap.procs[pid] = procSample{
				name: name,
				// UserTime and KernelTime are in 100-nanosecond units.
				cpuTime:  time.Duration(info.UserTime+info.KernelTime) * 100,
				memBytes: uint64(max(info.WorkingSetPrivateSize, 0)),
			}
		}

		if info.NextEntryOffset == 0 {
			break
		}

		offset += uintptr(info.NextEntryOffset)
	}

	return snap, nil
}

func querySystemProcessInformation() ([]byte, error) {
	size := uint32(512 * 1024)

	for range 8 {
		buf := make([]byte, size)

		var needed uint32

		err := windows.NtQuerySystemInformation(
			windows.SystemProcessInformation, unsafe.Pointer(&buf[0]), size, &needed,
		)
		if err == nil {
			return buf, nil
		}

		if err != windows.STATUS_INFO_LENGTH_MISMATCH {
			return nil, fmt.Errorf("NtQuerySystemInformation: %w", err)
		}

		// Processes may start between two calls: leave some headroom.
		size = max(needed, size) + 64*1024
	}

	return nil, fmt.Errorf("NtQuerySystemInformation: process list keeps growing")
}
