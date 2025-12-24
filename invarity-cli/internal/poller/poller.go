// Package poller provides utilities for polling async operations.
package poller

import (
	"context"
	"fmt"
	"io"
	"time"
)

// Config configures the polling behavior.
type Config struct {
	// InitialInterval is the initial polling interval.
	InitialInterval time.Duration
	// MaxInterval is the maximum polling interval (for exponential backoff).
	MaxInterval time.Duration
	// MaxWait is the maximum total time to wait.
	MaxWait time.Duration
	// Multiplier is the backoff multiplier (e.g., 2.0 for doubling).
	Multiplier float64
}

// DefaultConfig returns sensible defaults for polling.
func DefaultConfig() Config {
	return Config{
		InitialInterval: 1 * time.Second,
		MaxInterval:     10 * time.Second,
		MaxWait:         5 * time.Minute,
		Multiplier:      1.5,
	}
}

// Status represents the current status of a polled operation.
type Status string

const (
	StatusPending    Status = "PENDING"
	StatusCompiling  Status = "COMPILING"
	StatusReady      Status = "READY"
	StatusFailed     Status = "FAILED"
	StatusUnknown    Status = "UNKNOWN"
)

// IsTerminal returns true if the status is a terminal state.
func (s Status) IsTerminal() bool {
	return s == StatusReady || s == StatusFailed
}

// IsSuccess returns true if the status indicates success.
func (s Status) IsSuccess() bool {
	return s == StatusReady
}

// Result contains the final result of polling.
type Result struct {
	Status    Status
	Data      interface{}
	Error     error
	Attempts  int
	TotalTime time.Duration
}

// PollFunc is called on each poll iteration.
// It should return the current status, any associated data, and an error if the poll itself failed.
type PollFunc func(ctx context.Context) (Status, interface{}, error)

// ProgressFunc is called to report progress.
type ProgressFunc func(attempt int, elapsed time.Duration, status Status)

// Poller handles polling with exponential backoff.
type Poller struct {
	config   Config
	pollFunc PollFunc
	progress ProgressFunc
	output   io.Writer
}

// New creates a new Poller.
func New(pollFunc PollFunc, config Config) *Poller {
	return &Poller{
		config:   config,
		pollFunc: pollFunc,
	}
}

// WithProgress sets a progress callback.
func (p *Poller) WithProgress(fn ProgressFunc) *Poller {
	p.progress = fn
	return p
}

// WithOutput sets the output writer for progress messages.
func (p *Poller) WithOutput(w io.Writer) *Poller {
	p.output = w
	return p
}

// Poll starts polling and blocks until a terminal state or timeout.
func (p *Poller) Poll(ctx context.Context) Result {
	start := time.Now()
	interval := p.config.InitialInterval
	attempt := 0

	// Create a deadline context
	ctx, cancel := context.WithTimeout(ctx, p.config.MaxWait)
	defer cancel()

	var lastStatus Status
	var lastData interface{}

	for {
		attempt++

		// Call the poll function
		status, data, err := p.pollFunc(ctx)
		if err != nil {
			return Result{
				Status:    StatusFailed,
				Error:     err,
				Attempts:  attempt,
				TotalTime: time.Since(start),
			}
		}

		lastStatus = status
		lastData = data

		// Report progress
		if p.progress != nil {
			p.progress(attempt, time.Since(start), status)
		}

		// Check for terminal state
		if status.IsTerminal() {
			return Result{
				Status:    status,
				Data:      data,
				Attempts:  attempt,
				TotalTime: time.Since(start),
			}
		}

		// Calculate next interval with exponential backoff
		nextInterval := time.Duration(float64(interval) * p.config.Multiplier)
		if nextInterval > p.config.MaxInterval {
			nextInterval = p.config.MaxInterval
		}
		interval = nextInterval

		// Wait for next poll or context cancellation
		select {
		case <-ctx.Done():
			return Result{
				Status:    lastStatus,
				Data:      lastData,
				Error:     fmt.Errorf("polling timed out after %v (%d attempts)", time.Since(start), attempt),
				Attempts:  attempt,
				TotalTime: time.Since(start),
			}
		case <-time.After(interval):
			// Continue polling
		}
	}
}

// ParseStatus converts a string status to a Status type.
func ParseStatus(s string) Status {
	switch s {
	case "PENDING", "pending":
		return StatusPending
	case "COMPILING", "compiling", "PROCESSING", "processing":
		return StatusCompiling
	case "READY", "ready", "ACTIVE", "active", "SUCCESS", "success":
		return StatusReady
	case "FAILED", "failed", "ERROR", "error":
		return StatusFailed
	default:
		return StatusUnknown
	}
}

// DefaultProgressPrinter returns a progress function that prints to the given writer.
func DefaultProgressPrinter(w io.Writer) ProgressFunc {
	return func(attempt int, elapsed time.Duration, status Status) {
		fmt.Fprintf(w, "\r⏳ Waiting... [%s] (attempt %d, %s elapsed)", status, attempt, elapsed.Round(time.Second))
	}
}
