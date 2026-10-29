package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"job-alert/internal/jobs"
)

type Store struct {
	Jobs   map[string]SeenJob `json:"jobs"`
	Health Health             `json:"health"`
}

type Health struct {
	LastAttemptAt             time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt             time.Time `json:"last_success_at,omitempty"`
	LastFailureAt             time.Time `json:"last_failure_at,omitempty"`
	LastError                 string    `json:"last_error,omitempty"`
	LastHeartbeatAt           time.Time `json:"last_heartbeat_at,omitempty"`
	LastFailureNotificationAt time.Time `json:"last_failure_notification_at,omitempty"`
	Incident                  *Incident `json:"incident,omitempty"`
}

type Incident struct {
	StartedAt                      time.Time         `json:"started_at"`
	Attempts                       []IncidentAttempt `json:"attempts"`
	InitialNotificationAt          time.Time         `json:"initial_notification_at,omitempty"`
	MorningExhaustedDate           string            `json:"morning_exhausted_date,omitempty"`
	MorningExhaustedAt             time.Time         `json:"morning_exhausted_at,omitempty"`
	MorningExhaustedNotificationAt time.Time         `json:"morning_exhausted_notification_at,omitempty"`
	SummaryDate                    string            `json:"summary_date,omitempty"`
	SummaryNotificationAt          time.Time         `json:"summary_notification_at,omitempty"`
}

type IncidentAttempt struct {
	At         time.Time `json:"at"`
	Phase      string    `json:"phase"`
	Provenance string    `json:"provenance"`
	Error      string    `json:"error"`
}

type SeenJob struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Salary      string    `json:"salary"`
	Location    string    `json:"location"`
	Posted      string    `json:"posted"`
	FirstSeenAt time.Time `json:"first_seen_at"`
}

func Load(path string) (*Store, error) {
	store := &Store{Jobs: map[string]SeenJob{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, store); err != nil {
		return nil, err
	}
	if store.Jobs == nil {
		store.Jobs = map[string]SeenJob{}
	}
	return store, nil
}

func (s *Store) Seen(url string) bool {
	_, ok := s.Jobs[url]
	return ok
}

func (s *Store) Mark(job jobs.Job, now time.Time) {
	if s.Jobs == nil {
		s.Jobs = map[string]SeenJob{}
	}
	s.Jobs[job.URL] = SeenJob{
		Title:       job.Title,
		URL:         job.URL,
		Salary:      job.Salary,
		Location:    job.Location(),
		Posted:      job.Posted,
		FirstSeenAt: now,
	}
}

func (s *Store) BeginRun(now time.Time) {
	s.Health.LastAttemptAt = now
}

func (s *Store) RecordSuccess(now time.Time) {
	s.Health.LastSuccessAt = now
	s.Health.LastError = ""
}

func (s *Store) RecordFailure(now time.Time, err error) {
	s.Health.LastFailureAt = now
	s.Health.LastError = err.Error()
}

func (s *Store) RecordIncidentFailure(now time.Time, phase, provenance string, err error) {
	s.RecordFailure(now, err)
	if s.Health.Incident == nil {
		s.Health.Incident = &Incident{StartedAt: now}
	}
	s.Health.Incident.Attempts = append(s.Health.Incident.Attempts, IncidentAttempt{
		At:         now,
		Phase:      phase,
		Provenance: provenance,
		Error:      err.Error(),
	})
	const maxIncidentAttempts = 64
	if len(s.Health.Incident.Attempts) > maxIncidentAttempts {
		s.Health.Incident.Attempts = append([]IncidentAttempt(nil), s.Health.Incident.Attempts[len(s.Health.Incident.Attempts)-maxIncidentAttempts:]...)
	}
}

func (s *Store) MarkInitialIncidentNotification(now time.Time) {
	if s.Health.Incident != nil {
		s.Health.Incident.InitialNotificationAt = now
	}
}

func (s *Store) MarkMorningExhausted(now time.Time) {
	if s.Health.Incident == nil {
		return
	}
	date := now.Format("2006-01-02")
	if s.Health.Incident.MorningExhaustedDate != date {
		s.Health.Incident.MorningExhaustedNotificationAt = time.Time{}
	}
	s.Health.Incident.MorningExhaustedDate = date
	s.Health.Incident.MorningExhaustedAt = now
}

func (s *Store) MorningExhaustedOn(now time.Time) bool {
	return s.Health.Incident != nil && s.Health.Incident.MorningExhaustedDate == now.Format("2006-01-02")
}

func (s *Store) MarkMorningExhaustedNotification(now time.Time) {
	if s.Health.Incident != nil {
		s.Health.Incident.MorningExhaustedNotificationAt = now
	}
}

func (s *Store) SummarySentOn(now time.Time) bool {
	return s.Health.Incident != nil &&
		s.Health.Incident.SummaryDate == now.Format("2006-01-02") &&
		!s.Health.Incident.SummaryNotificationAt.IsZero()
}

func (s *Store) MarkSummaryNotification(now time.Time) {
	if s.Health.Incident == nil {
		return
	}
	s.Health.Incident.SummaryDate = now.Format("2006-01-02")
	s.Health.Incident.SummaryNotificationAt = now
}

func (s *Store) ClearIncident() {
	s.Health.Incident = nil
}

func (s *Store) HeartbeatDue(now time.Time, interval time.Duration) bool {
	last := s.Health.LastHeartbeatAt
	if last.IsZero() {
		return true
	}
	return !now.Before(last.Add(interval))
}

func (s *Store) MarkHeartbeat(now time.Time) {
	s.Health.LastHeartbeatAt = now
}

func (s *Store) FailureNotificationDue(now time.Time, cooldown time.Duration) bool {
	last := s.Health.LastFailureNotificationAt
	return last.IsZero() || now.Sub(last) >= cooldown
}

func (s *Store) MarkFailureNotification(now time.Time) {
	s.Health.LastFailureNotificationAt = now
}

func (s *Store) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
