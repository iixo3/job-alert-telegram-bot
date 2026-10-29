package main

import (
	"errors"
	"testing"
	"time"

	"job-alert/internal/state"
)

func TestClassifyRunWindow(t *testing.T) {
	location := time.FixedZone("UTC+2", 2*60*60)
	tests := []struct {
		hour int
		want runWindow
	}{
		{hour: 9, want: startupWindow},
		{hour: 10, want: morningWindow},
		{hour: 17, want: morningWindow},
		{hour: 18, want: eveningWindow},
		{hour: 23, want: eveningWindow},
	}
	for _, test := range tests {
		now := time.Date(2026, 10, 13, test.hour, 0, 0, 0, location)
		if got := classifyRunWindow(now); got != test.want {
			t.Fatalf("hour %d: window = %v, want %v", test.hour, got, test.want)
		}
	}
}

func TestMorningRecoveryUsesLatestActualAttempt(t *testing.T) {
	location := time.FixedZone("UTC+2", 2*60*60)
	date := time.Date(2026, 10, 13, 11, 0, 0, 0, location)
	incident := &state.Incident{Attempts: []state.IncidentAttempt{
		{At: date, Phase: "morning scan"},
		{At: date.Add(17 * time.Minute), Phase: "morning recovery 1/3"},
	}}
	want := date.Add(32 * time.Minute)
	if got := nextMorningRecoveryAt(incident, date); !got.Equal(want) {
		t.Fatalf("next recovery = %v, want %v", got, want)
	}
}

func TestMorningFailureCountIgnoresOtherDatesAndEvening(t *testing.T) {
	date := time.Date(2026, 10, 13, 11, 0, 0, 0, time.UTC)
	incident := &state.Incident{Attempts: []state.IncidentAttempt{
		{At: date, Phase: "morning scan"},
		{At: date.Add(15 * time.Minute), Phase: "morning recovery 1/3"},
		{At: date.Add(7 * time.Hour), Phase: "evening scan"},
		{At: date.Add(-24 * time.Hour), Phase: "morning scan"},
	}}
	if got := morningFailuresOn(incident, date); got != 2 {
		t.Fatalf("morning failures = %d, want 2", got)
	}
}

func TestDiagnoseFailure(t *testing.T) {
	tests := []struct {
		message string
		source  string
	}{
		{"read tcp: connection reset by peer", "Plexus host, hosting proxy, or network path"},
		{"lookup plexusrs.com: no such host", "local DNS, internet connection, or ISP resolver"},
		{"fetch: status 429", "Plexus rate limiting"},
		{"fetch: status 503", "Plexus website or hosting server"},
		{"Client.Timeout exceeded while awaiting headers", "Plexus website or network path timeout"},
		{"permission denied", "local installation or file permissions"},
	}
	for _, test := range tests {
		if got := diagnoseFailure(errors.New(test.message)).Source; got != test.source {
			t.Fatalf("%q: source = %q, want %q", test.message, got, test.source)
		}
	}
}
