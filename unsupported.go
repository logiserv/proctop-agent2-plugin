//go:build !windows && !linux

package main

import (
	"errors"
	"runtime"
)

const ephemeralPortStart = 49152

var errUnsupported = errors.New("proctop is not supported on " + runtime.GOOS)

func takeSnapshot() (*snapshot, error) { return nil, errUnsupported }

func readSockets() ([]socket, error) { return nil, errUnsupported }
