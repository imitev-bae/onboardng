package maintenance

import (
	"context"
	"testing"
	"time"
)

func TestShouldRun(t *testing.T) {
	s := NewMaintenanceService()

	// Every day at 03:00
	schedEveryday := Schedule{Hour: 3, Minute: 0}

	// Sundays at 04:30
	schedSunday := Schedule{Weekdays: []time.Weekday{time.Sunday}, Hour: 4, Minute: 30}

	// Mon and Thu at 05:00
	schedMonThu := Schedule{Weekdays: []time.Weekday{time.Monday, time.Thursday}, Hour: 5, Minute: 0}

	tests := []struct {
		name     string
		schedule Schedule
		now      time.Time
		lastRun  time.Time
		want     bool
	}{
		{
			name:     "Everyday - matching time",
			schedule: schedEveryday,
			now:      time.Date(2026, 2, 28, 3, 0, 0, 0, time.UTC),
			want:     true,
		},
		{
			name:     "Everyday - wrong hour",
			schedule: schedEveryday,
			now:      time.Date(2026, 2, 28, 4, 0, 0, 0, time.UTC),
			want:     false,
		},
		{
			name:     "Everyday - wrong minute",
			schedule: schedEveryday,
			now:      time.Date(2026, 2, 28, 3, 1, 0, 0, time.UTC),
			want:     false,
		},
		{
			name:     "Everyday - matching time but already run this minute",
			schedule: schedEveryday,
			now:      time.Date(2026, 2, 28, 3, 0, 10, 0, time.UTC),
			lastRun:  time.Date(2026, 2, 28, 3, 0, 5, 0, time.UTC),
			want:     false,
		},
		{
			name:     "Sunday - matching day and time",
			schedule: schedSunday,
			now:      time.Date(2026, 3, 1, 4, 30, 0, 0, time.UTC), // 2026-03-01 is Sunday
			want:     true,
		},
		{
			name:     "Sunday - wrong day (Saturday)",
			schedule: schedSunday,
			now:      time.Date(2026, 2, 28, 4, 30, 0, 0, time.UTC), // 2026-02-28 is Saturday
			want:     false,
		},
		{
			name:     "MonThu - Monday matching",
			schedule: schedMonThu,
			now:      time.Date(2026, 2, 23, 5, 0, 0, 0, time.UTC), // 2026-02-23 is Monday
			want:     true,
		},
		{
			name:     "MonThu - Thursday matching",
			schedule: schedMonThu,
			now:      time.Date(2026, 2, 26, 5, 0, 0, 0, time.UTC), // 2026-02-26 is Thursday
			want:     true,
		},
		{
			name:     "MonThu - Wednesday wrong",
			schedule: schedMonThu,
			now:      time.Date(2026, 2, 25, 5, 0, 0, 0, time.UTC), // 2026-02-25 is Wednesday
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &task{
				schedule: tt.schedule,
				lastRun:  tt.lastRun,
			}
			if got := s.shouldRun(task, tt.now); got != tt.want {
				t.Errorf("shouldRun() = %v, want %v", got, tt.want)
			}
		})
	}
}
func TestTaskExecution(t *testing.T) {
	s := NewMaintenanceService()

	executed := make(chan struct{})
	calledWithContext := false
	hasDeadline := false

	s.AddTask("test-task", Schedule{Hour: 10, Minute: 0}, func(ctx context.Context) error {
		calledWithContext = true
		deadline, ok := ctx.Deadline()
		if ok {
			hasDeadline = true
			// Check if deadline is roughly 1 hour from now
			remaining := time.Until(deadline)
			if remaining > 59*time.Minute && remaining < 61*time.Minute {
				// Success
			} else {
				t.Errorf("expected deadline ~1h, got %v", remaining)
			}
		}
		close(executed)
		return nil
	})

	// Manually trigger task check with matching time
	now := time.Date(2026, 2, 28, 10, 0, 0, 0, time.UTC)
	s.checkAndRunTasks(now)

	// Wait for task to execute
	select {
	case <-executed:
		if !calledWithContext {
			t.Error("task not called with context")
		}
		if !hasDeadline {
			t.Error("context has no deadline")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("task did not execute in time")
	}
}
