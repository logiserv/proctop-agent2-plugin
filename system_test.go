package main

import (
	"runtime"
	"strings"
	"testing"
)

// TestRealSystem runs every key against the machine running the tests.
func TestRealSystem(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("unsupported platform")
	}

	p := newPlugin()

	for _, tc := range []struct {
		key    string
		params []string
	}{
		{keyTop, []string{"5", "cpu"}},
		{keyTop, []string{"5", "mem"}},
		{keyTop, []string{"3", "cpu", "json"}},
		{keyConns, []string{"5"}},
		{keyListen, nil},
		{keyListen, []string{"json"}},
	} {
		v, err := p.Export(tc.key, tc.params, nil)
		if err != nil {
			t.Fatalf("%s%v: %v", tc.key, tc.params, err)
		}

		s, ok := v.(string)
		if !ok {
			t.Fatalf("%s%v: got %T, want string", tc.key, tc.params, v)
		}

		t.Logf("%s%v:\n%s", tc.key, tc.params, s)

		if tc.key == keyTop && len(tc.params) < 3 && !strings.HasPrefix(s, "Process") {
			t.Errorf("%s%v: expected a ranking, got %q", tc.key, tc.params, s)
		}
	}

	snap, err := p.sampler.fresh()
	if err != nil {
		t.Fatal(err)
	}

	if len(snap.procs) < 5 || snap.ncpu < 1 {
		t.Errorf("implausible snapshot: %d processes, %d CPUs", len(snap.procs), snap.ncpu)
	}
}
