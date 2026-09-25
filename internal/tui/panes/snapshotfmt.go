package panes

import (
	"fmt"

	"github.com/colbytimm/alchemist/internal/snapshot"
)

// FormatUsage is a store's cost beside its exports': "30 snapshots · 30.0 GB
// of items · 460.0 MB on disk · 65× smaller".
func FormatUsage(u snapshot.Usage) string {
	text := fmt.Sprintf("%s · %s of items · %s on disk", snapshotCount(u.Snapshots), FormatBytes(u.LogicalBytes), FormatBytes(u.OnDisk()))
	if ratio := u.Ratio(); ratio >= 1 {
		text += fmt.Sprintf(" · %.0f× smaller", ratio)
	}
	return text
}

func snapshotCount(n int) string {
	if n == 1 {
		return "1 snapshot"
	}
	return fmt.Sprintf("%d snapshots", n)
}
