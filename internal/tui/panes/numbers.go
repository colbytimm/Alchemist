package panes

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const bytesPerUnit = 1024

// FormatCount writes n with its thousands grouped: 30,112.
func FormatCount(n int64) string {
	digits := strconv.FormatInt(n, 10)
	sign := ""
	if n < 0 {
		sign, digits = "-", digits[1:]
	}
	var grouped strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}
	return sign + grouped.String()
}

// FormatCharge writes a request charge to the cent, grouped: 1,912.40.
func FormatCharge(charge float64) string {
	cents := int64(charge*100 + 0.5)
	return fmt.Sprintf("%s.%02d", FormatCount(cents/100), cents%100)
}

// FormatBytes writes a size in the largest unit it fills.
func FormatBytes(n int64) string {
	switch {
	case n < bytesPerUnit:
		return fmt.Sprintf("%d B", n)
	case n < bytesPerUnit*bytesPerUnit:
		return fmt.Sprintf("%.1f KB", float64(n)/bytesPerUnit)
	case n < bytesPerUnit*bytesPerUnit*bytesPerUnit:
		return fmt.Sprintf("%.1f MB", float64(n)/(bytesPerUnit*bytesPerUnit))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(bytesPerUnit*bytesPerUnit*bytesPerUnit))
}

// formatWait writes a wait to the second, largest unit first: 1m 24s.
func formatWait(d time.Duration) string {
	d = d.Round(time.Second)
	hours, minutes, seconds := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	switch {
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

// times writes how often something happened, the way it is said.
func times(n int) string {
	switch n {
	case 1:
		return "once"
	case 2:
		return "twice"
	}
	return fmt.Sprintf("%d times", n)
}
