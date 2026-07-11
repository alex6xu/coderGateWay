package cron

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/alex/codegateway/internal/model"
	"github.com/google/uuid"
)

// Scheduler manages cron jobs
type Scheduler struct {
	db       *sql.DB
	jobs     map[string]*model.CronJob
	mu       sync.RWMutex
	stopCh   chan struct{}
	interval time.Duration
	handler  func(prompt string)
}

// NewScheduler creates a new scheduler
func NewScheduler(db *sql.DB, interval time.Duration, handler func(prompt string)) *Scheduler {
	return &Scheduler{
		db:       db,
		jobs:     make(map[string]*model.CronJob),
		stopCh:   make(chan struct{}),
		interval: interval,
		handler:  handler,
	}
}

// Start starts the scheduler
func (s *Scheduler) Start() error {
	// Load jobs from database
	if err := s.loadJobs(); err != nil {
		return fmt.Errorf("failed to load jobs: %w", err)
	}

	// Start scheduler loop
	go s.loop()

	log.Println("Cron scheduler started")
	return nil
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	close(s.stopCh)
	log.Println("Cron scheduler stopped")
}

// loop runs the scheduler loop
func (s *Scheduler) loop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.tick()
		case <-s.stopCh:
			return
		}
	}
}

// tick processes scheduled jobs
func (s *Scheduler) tick() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	for _, job := range s.jobs {
		if !job.Enabled {
			continue
		}

		if job.NextRun != nil && now.After(*job.NextRun) {
			// Execute job
			go s.executeJob(job)

			// Update next run time
			nextRun := s.calculateNextRun(job.Cron)
			job.NextRun = &nextRun
			job.LastRun = &now

			// Update database
			s.updateJob(job)
		}
	}
}

// executeJob executes a cron job
func (s *Scheduler) executeJob(job *model.CronJob) {
	log.Printf("Executing cron job: %s", job.ID)

	if s.handler != nil {
		s.handler(job.Prompt)
	}
}

// calculateNextRun calculates the next run time
func (s *Scheduler) calculateNextRun(cronExpr string) time.Time {
	// Simple implementation: run every hour
	// TODO: Implement proper cron expression parsing
	return time.Now().Add(time.Hour)
}

// loadJobs loads jobs from database
func (s *Scheduler) loadJobs() error {
	rows, err := s.db.Query("SELECT id, cron, prompt, enabled, last_run, next_run, created_at FROM cron_jobs")
	if err != nil {
		return fmt.Errorf("failed to load jobs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var job model.CronJob
		err := rows.Scan(&job.ID, &job.Cron, &job.Prompt, &job.Enabled, &job.LastRun, &job.NextRun, &job.CreatedAt)
		if err != nil {
			return fmt.Errorf("failed to scan job: %w", err)
		}
		s.jobs[job.ID] = &job
	}

	return nil
}

// updateJob updates a job in database
func (s *Scheduler) updateJob(job *model.CronJob) error {
	_, err := s.db.Exec(
		"UPDATE cron_jobs SET last_run = ?, next_run = ? WHERE id = ?",
		job.LastRun, job.NextRun, job.ID,
	)
	return err
}

// Schedule schedules a new cron job
func (s *Scheduler) Schedule(cron string, prompt string) (*model.CronJob, error) {
	nextRun := s.calculateNextRun(cron)

	job := &model.CronJob{
		ID:        uuid.New().String(),
		Cron:      cron,
		Prompt:    prompt,
		Enabled:   true,
		NextRun:   &nextRun,
		CreatedAt: time.Now(),
	}

	_, err := s.db.Exec(
		"INSERT INTO cron_jobs (id, cron, prompt, enabled, next_run, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		job.ID, job.Cron, job.Prompt, job.Enabled, job.NextRun, job.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to schedule job: %w", err)
	}

	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()

	return job, nil
}

// Delete deletes a cron job
func (s *Scheduler) Delete(id string) error {
	_, err := s.db.Exec("DELETE FROM cron_jobs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete job: %w", err)
	}

	s.mu.Lock()
	delete(s.jobs, id)
	s.mu.Unlock()

	return nil
}

// Enable enables a cron job
func (s *Scheduler) Enable(id string) error {
	_, err := s.db.Exec("UPDATE cron_jobs SET enabled = true WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to enable job: %w", err)
	}

	s.mu.Lock()
	if job, ok := s.jobs[id]; ok {
		job.Enabled = true
	}
	s.mu.Unlock()

	return nil
}

// Disable disables a cron job
func (s *Scheduler) Disable(id string) error {
	_, err := s.db.Exec("UPDATE cron_jobs SET enabled = false WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to disable job: %w", err)
	}

	s.mu.Lock()
	if job, ok := s.jobs[id]; ok {
		job.Enabled = false
	}
	s.mu.Unlock()

	return nil
}

// List returns all cron jobs
func (s *Scheduler) List() []*model.CronJob {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*model.CronJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}

	return jobs
}

// Get returns a cron job by ID
func (s *Scheduler) Get(id string) (*model.CronJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, ok := s.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job not found: %s", id)
	}

	return job, nil
}
