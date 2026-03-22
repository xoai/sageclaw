package pipeline

import (
	"testing"
	"time"
)

func TestCronRunner_IntervalForSchedule(t *testing.T) {
	cr := &CronRunner{}

	tests := []struct {
		schedule string
		expected time.Duration
	}{
		{"@hourly", time.Hour},
		{"@daily", 24 * time.Hour},
		{"@weekly", 7 * 24 * time.Hour},
		{"@every 5m", 5 * time.Minute},
		{"@every 1h", time.Hour},
		{"@every 30s", 30 * time.Second},
		{"*/5 * * * *", 5 * time.Minute},
		{"*/15 * * * *", 15 * time.Minute},
		{"invalid", 0},
	}

	for _, tt := range tests {
		got := cr.intervalForSchedule(tt.schedule)
		if got != tt.expected {
			t.Errorf("intervalForSchedule(%q) = %v, want %v", tt.schedule, got, tt.expected)
		}
	}
}
