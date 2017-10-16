package humanbytes

import (
	"fmt"
	"strconv"
	"strings"
)

var (
	suffixes = map[string]int64{
		"B": 0,
		"K": 1024,
		"M": 1024 * 1024,
		"G": 1024 * 1024 * 1024,
		"T": 1024 * 1024 * 1024 * 1024,
		"P": 1024 * 1024 * 1024 * 1024 * 1024,

		"KiB": 1024,
		"MiB": 1024 * 1024,
		"GiB": 1024 * 1024 * 1024,
		"TiB": 1024 * 1024 * 1024 * 1024,
		"PiB": 1024 * 1024 * 1024 * 1024 * 1024,

		"KB": 1000,
		"MB": 1000 * 1000,
		"GB": 1000 * 1000 * 1000,
		"TB": 1000 * 1000 * 1000 * 1000,
		"PB": 1000 * 1000 * 1000 * 1000 * 1000,
	}

	suffixOrder = []string{
		"P", "PiB", "PB",
		"T", "TiB", "TB",
		"G", "GiB", "GB",
		"M", "MiB", "MB",
		"K", "KiB", "KB",
		"B"}
)

func Format(b int64) string {
	for _, suffix := range suffixOrder {
		divisor := suffixes[suffix]
		if b < divisor {
			continue
		}

		if suffix == "B" {
			return fmt.Sprintf("%dB", b)
		}
		return fmt.Sprintf("%.2f%s", float64(b)/float64(divisor), suffix)
	}
	return fmt.Sprintf("%dB", b)
}

func Parse(s string) (int64, error) {
	for suffix, factor := range suffixes {
		if !strings.HasSuffix(s, suffix) {
			continue
		}
		b, err := strconv.ParseInt(strings.TrimSuffix(s, suffix), 0, 64)
		if err != nil {
			return 0, err
		}
		return b * factor, nil
	}
	// No suffix: try parsing as a byte value
	return strconv.ParseInt(s, 0, 64)
}
