package pipeline

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/store"
)

const cronCheckInterval = 30 * time.Second

// CronRunner checks for due cron jobs and schedules them.
type CronRunner struct {
	store     store.Store
	scheduler Scheduler
	interval  time.Duration
	cancel    context.CancelFunc
}

// NewCronRunner creates a cron runner.
func NewCronRunner(store store.Store, scheduler Scheduler) *CronRunner {
	return &CronRunner{
		store:     store,
		scheduler: scheduler,
		interval:  cronCheckInterval,
	}
}

// Start begins the cron check loop.
func (cr *CronRunner) Start(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	cr.cancel = cancel
	go cr.loop(runCtx)
}

// Stop stops the cron runner.
func (cr *CronRunner) Stop() {
	if cr.cancel != nil {
		cr.cancel()
	}
}

func (cr *CronRunner) loop(ctx context.Context) {
	ticker := time.NewTicker(cr.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cr.checkAndRun(ctx)
		}
	}
}

func (cr *CronRunner) checkAndRun(ctx context.Context) {
	jobs, err := cr.store.ListCronJobs(ctx)
	if err != nil {
		log.Printf("cron: failed to list jobs: %v", err)
		return
	}

	now := time.Now().UTC()
	for _, job := range jobs {
		if !job.Enabled {
			continue
		}

		nextRun := cr.computeNextRun(job.Schedule, now)
		if nextRun.IsZero() {
			continue
		}

		// Check if job is due (next_run is in the past or within the check interval).
		var lastRun time.Time
		if lr, err := cr.store.GetCronLastRun(ctx, job.ID); err == nil {
			lastRun = lr
		}

		if !lastRun.IsZero() && now.Sub(lastRun) < cr.intervalForSchedule(job.Schedule) {
			continue // Not due yet.
		}

		// Schedule the job.
		log.Printf("cron: firing job %s (%s)", job.ID[:8], job.Schedule)
		cr.scheduler.Schedule(ctx, LaneCron, RunRequest{
			AgentID: job.AgentID,
			Messages: []canonical.Message{
				{Role: "user", Content: []canonical.Content{{Type: "text", Text: job.Prompt}}},
			},
			Lane: LaneCron,
		})

		// Update last_run.
		cr.store.UpdateCronLastRun(ctx, job.ID, now)
	}
}

func (cr *CronRunner) computeNextRun(schedule string, now time.Time) time.Time {
	interval := cr.intervalForSchedule(schedule)
	if interval == 0 {
		return time.Time{}
	}
	return now.Add(interval)
}

func (cr *CronRunner) intervalForSchedule(schedule string) time.Duration {
	schedule = strings.TrimSpace(schedule)

	switch schedule {
	case "@hourly":
		return time.Hour
	case "@daily":
		return 24 * time.Hour
	case "@weekly":
		return 7 * 24 * time.Hour
	case "@monthly":
		return 30 * 24 * time.Hour
	}

	if strings.HasPrefix(schedule, "@every ") {
		durStr := strings.TrimPrefix(schedule, "@every ")
		d, err := parseDuration(durStr)
		if err == nil {
			return d
		}
	}

	// Basic cron: */N * * * * (every N minutes).
	if strings.HasPrefix(schedule, "*/") {
		parts := strings.Fields(schedule)
		if len(parts) >= 1 {
			nStr := strings.TrimPrefix(parts[0], "*/")
			n, err := strconv.Atoi(nStr)
			if err == nil && n > 0 {
				return time.Duration(n) * time.Minute
			}
		}
	}

	return 0
}

// parseDuration parses durations like "5m", "1h", "30s".
func parseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}
