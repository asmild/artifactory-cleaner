package util

import (
	"encoding/json"
	"fmt"
)

func Contains[E comparable](s []E, v E) bool {
	for i := range s {
		if v == s[i] {
			return true
		}
	}
	return false
}

func Debug(enabled bool, tag string, input ...interface{}) {
	if enabled {
		out, err := json.Marshal(input)
		if err != nil {
			panic(err)
		}

		fmt.Println(fmt.Sprintf("[DEBUG] %s: %s", tag, string(out)))
	}
}

func FormatSize(size int64) string {
	const kb = 1024
	const mb = kb * 1024
	if size >= mb {
		return fmt.Sprintf("%.2f MB", float64(size)/mb)
	} else if size >= kb {
		return fmt.Sprintf("%.2f KB", float64(size)/kb)
	} else {
		return fmt.Sprintf("%d B", size)
	}
}

func HumanReadableSizeFormat(size int64) string {
	const (
		b = 1 << (10 * iota) // 1 byte
		kb
		mb
		gb
		tb
		pb
	)

	var unit string
	value := float64(size)

	switch {
	case size >= pb:
		unit = "PB"
		value /= pb
	case size >= tb:
		unit = "TB"
		value /= tb
	case size >= gb:
		unit = "GB"
		value /= gb
	case size >= mb:
		unit = "MB"
		value /= mb
	case size >= kb:
		unit = "KB"
		value /= kb
	default:
		unit = "B"
	}

	return fmt.Sprintf("%.2f %s", value, unit)
}
