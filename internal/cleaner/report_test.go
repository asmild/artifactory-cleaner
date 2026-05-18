package cleaner

import (
	"testing"
	"time"
)

func TestFormatSize(t *testing.T) {
	const kb = int64(1024)
	const mb = kb * 1024

	cases := []struct {
		size int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{kb, "1.00 KB"},
		{kb + kb/2, "1.50 KB"},
		{mb, "1.00 MB"},
		{mb * 2, "2.00 MB"},
	}

	for _, c := range cases {
		if got := formatSize(c.size); got != c.want {
			t.Errorf("formatSize(%d) = %q, want %q", c.size, got, c.want)
		}
	}
}

func TestFormatTimestamp_Nil(t *testing.T) {
	if got := formatTimestamp(nil); got != "- never -" {
		t.Errorf("formatTimestamp(nil) = %q, want %q", got, "- never -")
	}
}

func TestFormatTimestamp_NonNil(t *testing.T) {
	ts := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	want := "2024-06-15 10:30:00"
	if got := formatTimestamp(&ts); got != want {
		t.Errorf("formatTimestamp = %q, want %q", got, want)
	}
}