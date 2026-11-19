package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
)

// The text formats are meant for trigger operational data and notifications:
// aligned columns, one entry per line, and no curly braces (some notification
// pipelines treat "{...}" as an unresolved Zabbix macro).

func formatRanking(r ranking, f outputFormat) string {
	if f == formatJSON {
		return toJSON(r)
	}

	if len(r.Top) == 0 {
		return ""
	}

	header := "CPU"
	if r.Unit == "MB" {
		header = "Memory"
	}

	rows := [][2]string{{"Process", header}}
	for _, e := range r.Top {
		rows = append(rows, [2]string{withInstances(e.Name, e.Instances), formatValue(e.Value, r.Unit)})
	}

	if r.Other > 0 {
		rows = append(rows, [2]string{"Other processes", formatValue(r.Other, r.Unit)})
	}

	// Names left-aligned, values right-aligned.
	nameWidth, valueWidth := 0, 0
	for _, row := range rows {
		nameWidth = max(nameWidth, len(row[0]))
		valueWidth = max(valueWidth, len(row[1]))
	}

	lines := make([]string, len(rows))
	for i, row := range rows {
		lines[i] = fmt.Sprintf("%-*s  %*s", nameWidth, row[0], valueWidth, row[1])
	}

	return strings.Join(lines, "\n")
}

func formatConns(entries []connEntry, f outputFormat) string {
	if f == formatJSON {
		if entries == nil {
			entries = []connEntry{}
		}

		return toJSON(entries)
	}

	if len(entries) == 0 {
		return ""
	}

	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	fmt.Fprintln(w, "Process\tConnections\tRemote hosts\tMain remote hosts")

	for _, e := range entries {
		remotes := make([]string, 0, len(e.TopRemotes))
		for _, r := range e.TopRemotes {
			remotes = append(remotes, fmt.Sprintf("%s (%d)", r.Address, r.Connections))
		}

		fmt.Fprintf(w, "%s\t%d\t%d\t%s\n", e.Process, e.Connections, e.Remotes, strings.Join(remotes, ", "))
	}

	_ = w.Flush()

	return strings.TrimRight(buf.String(), "\n")
}

func formatListen(entries []listenEntry, f outputFormat) string {
	if f == formatJSON {
		if entries == nil {
			entries = []listenEntry{}
		}

		return toJSON(entries)
	}

	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("%s/%d %s [%s]", e.Proto, e.Port, e.Process, strings.Join(e.Addresses, ", ")))
	}

	return strings.Join(lines, "\n")
}

func withInstances(name string, n int) string {
	if n > 1 {
		return fmt.Sprintf("%s (%d)", name, n)
	}

	return name
}

func formatValue(v float64, unit string) string {
	if unit == "MB" {
		if v >= 1024 {
			return fmt.Sprintf("%.1f GB", v/1024)
		}

		return fmt.Sprintf("%.0f MB", v)
	}

	return fmt.Sprintf("%.2f%%", v)
}

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}

	return string(b)
}
