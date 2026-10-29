package state

import (
	"path/filepath"
	"testing"
	"time"

	"job-alert/internal/jobs"
)

func TestStoreSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seen.json")
	store := &Store{Jobs: map[string]SeenJob{}}
	job := jobs.Job{
		Title:    "Growth Lead",
		URL:      "https://plexusrs.com/jobs/growth-lead-1/",
		Salary:   "$140k",
		WorkMode: "Remote",
		Region:   "Global",
		Posted:   "11.06.26",
	}
	store.Mark(job, time.Unix(100, 0).UTC())
	store.BeginRun(time.Unix(200, 0).UTC())
	store.RecordSuccess(time.Unix(201, 0).UTC())
	store.MarkHeartbeat(time.Unix(202, 0).UTC())

	if err := store.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Seen(job.URL) {
		t.Fatal("job should be seen")
	}
	if loaded.Jobs[job.URL].Location != "Remote - Global" {
		t.Fatalf("location = %q", loaded.Jobs[job.URL].Location)
	}
	if got := loaded.Health.LastSuccessAt; !got.Equal(time.Unix(201, 0).UTC()) {
		t.Fatalf("last success = %v", got)
	}
}

func TestHeartbeatDueAfterInterval(t *testing.T) {
	store := &Store{}
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	interval := 7 * 24 * time.Hour

	if !store.HeartbeatDue(now, interval) {
		t.Fatal("first heartbeat should be due")
	}
	store.MarkHeartbeat(now)
	if store.HeartbeatDue(now.Add(interval-time.Second), interval) {
		t.Fatal("heartbeat should be suppressed before interval elapses")
	}
	if !store.HeartbeatDue(now.Add(interval), interval) {
		t.Fatal("heartbeat should be due when interval elapses")
	}
}

func TestFailureNotificationCooldown(t *testing.T) {
	store := &Store{}
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)

	if !store.FailureNotificationDue(now, 24*time.Hour) {
		t.Fatal("first failure notification should be due")
	}
	store.MarkFailureNotification(now)
	if store.FailureNotificationDue(now.Add(23*time.Hour), 24*time.Hour) {
		t.Fatal("failure notification should be suppressed during cooldown")
	}
	if !store.FailureNotificationDue(now.Add(24*time.Hour), 24*time.Hour) {
		t.Fatal("failure notification should be due after cooldown")
	}
}

func TestIncidentPersistsAndClears(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seen.json")
	store := &Store{Jobs: map[string]SeenJob{}}
	started := time.Date(2026, 10, 13, 11, 0, 0, 0, time.UTC)
	store.RecordIncidentFailure(started, "morning scan", "Plexus server", assertError("status 503"))
	store.MarkInitialIncidentNotification(started.Add(time.Minute))
	store.MarkMorningExhausted(started.Add(45 * time.Minute))
	store.MarkMorningExhaustedNotification(started.Add(46 * time.Minute))

	if err := store.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Health.Incident == nil {
		t.Fatal("incident should persist")
	}
	if len(loaded.Health.Incident.Attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(loaded.Health.Incident.Attempts))
	}
	if !loaded.MorningExhaustedOn(started) {
		t.Fatal("morning exhaustion should persist for the same date")
	}

	loaded.ClearIncident()
	if loaded.Health.Incident != nil {
		t.Fatal("incident should be cleared")
	}
}

func TestIncidentAttemptsAreCapped(t *testing.T) {
	store := &Store{}
	now := time.Date(2026, 10, 13, 11, 0, 0, 0, time.UTC)
	for i := 0; i < 70; i++ {
		store.RecordIncidentFailure(now.Add(time.Duration(i)*time.Minute), "retry", "network", assertError("failed"))
	}
	if got := len(store.Health.Incident.Attempts); got != 64 {
		t.Fatalf("attempts = %d, want 64", got)
	}
	if got := store.Health.Incident.Attempts[0].At; !got.Equal(now.Add(6 * time.Minute)) {
		t.Fatalf("first retained attempt = %v", got)
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
