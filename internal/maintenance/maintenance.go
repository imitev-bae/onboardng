package maintenance

import (
	"context"
	"log/slog"
	"slices"
	"time"
)

// Schedule defines when a maintenance task should run.
type Schedule struct {
	// Weekdays is a list of days when the task should run.
	// If empty, the task runs every day.
	Weekdays []time.Weekday
	// Hour is the hour of the day (0-23).
	Hour int
	// Minute is the minute of the hour (0-59).
	Minute int
}

type task struct {
	name     string
	schedule Schedule
	fn       func(context.Context) error
	lastRun  time.Time
}

// MaintenanceService manages and executes periodic maintenance tasks.
type MaintenanceService struct {
	tasks []*task
}

// NewMaintenanceService creates a new MaintenanceService.
func NewMaintenanceService() *MaintenanceService {
	return &MaintenanceService{}
}

// AddTask registers a new maintenance task with a scheduling policy.
func (s *MaintenanceService) AddTask(name string, schedule Schedule, fn func(context.Context) error) {
	s.tasks = append(s.tasks, &task{
		name:     name,
		schedule: schedule,
		fn:       fn,
	})
}

// Start runs the maintenance loop in the background.
func (s *MaintenanceService) Start() {
	slog.Info("Starting maintenance service")
	go s.run()
}

func (s *MaintenanceService) run() {
	// Check every 30 seconds to ensure we don't miss a minute.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for t := range ticker.C {
		s.checkAndRunTasks(t)
	}
}

func (s *MaintenanceService) checkAndRunTasks(now time.Time) {
	for _, t := range s.tasks {
		if s.shouldRun(t, now) {
			// Mark as last run immediately to avoid concurrent double execution
			t.lastRun = now
			go func(currentTask *task) {
				// Create a context with a 1-hour deadline for the task
				taskCtx, cancel := context.WithTimeout(context.Background(), 1*time.Hour)
				defer cancel()

				slog.Info("Running maintenance task", "name", currentTask.name)
				if err := currentTask.fn(taskCtx); err != nil {
					slog.Error("Maintenance task failed", "name", currentTask.name, "error", err)
				}
				slog.Info("Maintenance task completed", "name", currentTask.name)
			}(t)
		}
	}
}

func (s *MaintenanceService) shouldRun(t *task, now time.Time) bool {
	// Check if it's the right time
	if now.Hour() != t.schedule.Hour || now.Minute() != t.schedule.Minute {
		return false
	}

	// Only run once per minute
	if !t.lastRun.IsZero() &&
		t.lastRun.Year() == now.Year() &&
		t.lastRun.Month() == now.Month() &&
		t.lastRun.Day() == now.Day() &&
		t.lastRun.Hour() == now.Hour() &&
		t.lastRun.Minute() == now.Minute() {
		return false
	}

	// Check if it's the right day of the week
	if len(t.schedule.Weekdays) > 0 {
		found := slices.Contains(t.schedule.Weekdays, now.Weekday())
		if !found {
			return false
		}
	}

	return true
}
