package job

import (
	"crypto/rand"
	"fmt"
	"time"
)

type Job struct {
	ID          string
	Status      Status
	WebhookURL  string
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
	RunTime     float64
	QueueTime   float64
}

func New(webhookURL string) *Job {
	return &Job{
		ID:         newID(),
		Status:     StatusQueued,
		WebhookURL: webhookURL,
		CreatedAt:  time.Now(),
	}
}

func (j *Job) Start() {
	now := time.Now()
	j.Status = StatusRunning
	j.StartedAt = &now
	j.QueueTime = now.Sub(j.CreatedAt).Seconds()
}

func (j *Job) Complete() {
	now := time.Now()
	j.Status = StatusDone
	j.CompletedAt = &now
	if j.StartedAt != nil {
		j.RunTime = now.Sub(*j.StartedAt).Seconds()
	}
}

func (j *Job) Fail() {
	now := time.Now()
	j.Status = StatusFailed
	j.CompletedAt = &now
}

func (j *Job) Validate() error {
	if j.ID == "" {
		return fmt.Errorf("job ID cannot be empty")
	}
	return nil
}

// newID gera um UUID v4 usando crypto/rand — sem dependência externa.
func newID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
