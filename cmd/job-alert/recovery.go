package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"job-alert/internal/config"
	"job-alert/internal/jobs"
	"job-alert/internal/state"
)

const (
	morningRecoveryInterval = 15 * time.Minute
	maxMorningFetches       = 4 // Initial scan plus three recovery scans.
)

type runWindow int

const (
	startupWindow runWindow = iota
	morningWindow
	eveningWindow
)

type failureDiagnosis struct {
	Source string
	Action string
}

func fetchMatchesWithRecovery(ctx context.Context, cfg config.Config, store *state.Store, force bool) ([]jobs.Job, []jobs.Job, error) {
	startedAt := time.Now()
	window := classifyRunWindow(startedAt)

	if !force && window == morningWindow && morningFailuresOn(store.Health.Incident, startedAt) >= maxMorningFetches {
		store.MarkMorningExhausted(startedAt)
		if err := store.Save(cfg.StatePath); err != nil {
			return nil, nil, err
		}
		notifyErr := notifyMorningExhausted(ctx, cfg, store, startedAt)
		return nil, nil, errors.Join(lastIncidentError(store.Health.Incident), notifyErr)
	}

	phase := initialPhase(window, morningFailuresOn(store.Health.Incident, startedAt))
	if force {
		phase = "manual recovery scan"
	}
	matches, allJobs, fetchErr := fetchMatches(ctx, cfg)
	if fetchErr == nil {
		return matches, allJobs, nil
	}
	if err := recordFetchFailure(cfg, store, time.Now(), phase, fetchErr); err != nil {
		return nil, nil, errors.Join(fetchErr, err)
	}
	if force {
		notifyErr := notifyInitialIncident(ctx, cfg, store, time.Now(), "wait for the next scheduled scan, or run another manual forced scan after correcting the reported issue")
		return nil, nil, errors.Join(fetchErr, notifyErr)
	}

	switch window {
	case startupWindow:
		notifyErr := notifyInitialIncident(ctx, cfg, store, time.Now(), "next scheduled scan at 11:00")
		return nil, nil, errors.Join(fetchErr, notifyErr)
	case eveningWindow:
		if store.MorningExhaustedOn(time.Now()) {
			notifyErr := notifyDailySummary(ctx, cfg, store, time.Now())
			return nil, nil, errors.Join(fetchErr, notifyErr)
		}
		notifyErr := notifyInitialIncident(ctx, cfg, store, time.Now(), "next scheduled scan at 11:00 tomorrow")
		return nil, nil, errors.Join(fetchErr, notifyErr)
	case morningWindow:
		remaining := maxMorningFetches - morningFailuresOn(store.Health.Incident, startedAt)
		plan := fmt.Sprintf("%d more full recovery scan(s) at 15-minute intervals, then wait for 18:00", remaining)
		if err := notifyInitialIncident(ctx, cfg, store, time.Now(), plan); err != nil {
			fmt.Printf("warning: send initial incident notification: %v\n", err)
		}
	}

	return runMorningRecovery(ctx, cfg, store, startedAt, fetchErr)
}

func runMorningRecovery(ctx context.Context, cfg config.Config, store *state.Store, startedAt time.Time, lastFetchErr error) ([]jobs.Job, []jobs.Job, error) {
	cutoff := eveningCutoff(startedAt)

	for failures := morningFailuresOn(store.Health.Incident, startedAt); failures < maxMorningFetches; failures = morningFailuresOn(store.Health.Incident, startedAt) {
		target := nextMorningRecoveryAt(store.Health.Incident, startedAt)
		if !target.Before(cutoff) {
			break
		}
		if delay := time.Until(target); delay > 0 {
			fmt.Printf("morning recovery %d/3 scheduled for %s\n", failures, target.Format("15:04 MST"))
			if err := sleepContext(ctx, delay); err != nil {
				return nil, nil, errors.Join(lastFetchErr, err)
			}
		}

		now := time.Now()
		if !sameLocalDate(now, startedAt) {
			return nil, nil, lastFetchErr
		}
		if !now.Before(cutoff) {
			return runEveningRecovery(ctx, cfg, store, startedAt, lastFetchErr)
		}

		store.BeginRun(now)
		if err := store.Save(cfg.StatePath); err != nil {
			return nil, nil, errors.Join(lastFetchErr, err)
		}
		phase := initialPhase(morningWindow, failures)
		matches, allJobs, fetchErr := fetchMatches(ctx, cfg)
		if fetchErr == nil {
			return matches, allJobs, nil
		}
		lastFetchErr = fetchErr
		if err := recordFetchFailure(cfg, store, time.Now(), phase, fetchErr); err != nil {
			return nil, nil, errors.Join(fetchErr, err)
		}
	}

	if !time.Now().Before(cutoff) && sameLocalDate(time.Now(), startedAt) {
		return runEveningRecovery(ctx, cfg, store, startedAt, lastFetchErr)
	}

	now := time.Now()
	store.MarkMorningExhausted(now)
	if err := store.Save(cfg.StatePath); err != nil {
		return nil, nil, errors.Join(lastFetchErr, err)
	}
	notifyErr := notifyMorningExhausted(ctx, cfg, store, now)
	return nil, nil, errors.Join(lastFetchErr, notifyErr)
}

func runEveningRecovery(ctx context.Context, cfg config.Config, store *state.Store, morningDate time.Time, lastFetchErr error) ([]jobs.Job, []jobs.Job, error) {
	now := time.Now()
	if !sameLocalDate(now, morningDate) {
		return nil, nil, lastFetchErr
	}
	store.MarkMorningExhausted(now)
	store.BeginRun(now)
	if err := store.Save(cfg.StatePath); err != nil {
		return nil, nil, errors.Join(lastFetchErr, err)
	}

	matches, allJobs, fetchErr := fetchMatches(ctx, cfg)
	if fetchErr == nil {
		return matches, allJobs, nil
	}
	if err := recordFetchFailure(cfg, store, time.Now(), "evening scan", fetchErr); err != nil {
		return nil, nil, errors.Join(fetchErr, err)
	}
	notifyErr := notifyDailySummary(ctx, cfg, store, time.Now())
	return nil, nil, errors.Join(fetchErr, notifyErr)
}

func recordFetchFailure(cfg config.Config, store *state.Store, at time.Time, phase string, fetchErr error) error {
	diagnosis := diagnoseFailure(fetchErr)
	store.RecordIncidentFailure(at, phase, diagnosis.Source, fetchErr)
	return store.Save(cfg.StatePath)
}

func notifyInitialIncident(ctx context.Context, cfg config.Config, store *state.Store, now time.Time, automaticPlan string) error {
	incident := store.Health.Incident
	if incident == nil || !incident.InitialNotificationAt.IsZero() {
		return nil
	}
	attempt := incident.Attempts[len(incident.Attempts)-1]
	message := fmt.Sprintf(
		"Job monitor issue detected\nChecked: %s\nProbable source: %s\nError: %s\nLast successful scan: %s\nAutomatic action: %s.\nAction now: none.",
		now.Format("02.01.06 15:04 MST"), attempt.Provenance, compactText(attempt.Error, 500),
		formatOptionalTime(store.Health.LastSuccessAt), automaticPlan,
	)
	if err := sendOperationalNotice(ctx, cfg, message); err != nil {
		return err
	}
	store.MarkInitialIncidentNotification(now)
	store.MarkFailureNotification(now)
	return store.Save(cfg.StatePath)
}

func notifyMorningExhausted(ctx context.Context, cfg config.Config, store *state.Store, now time.Time) error {
	incident := store.Health.Incident
	if incident == nil || (incident.MorningExhaustedDate == now.Format("2006-01-02") && !incident.MorningExhaustedNotificationAt.IsZero()) {
		return nil
	}
	store.MarkMorningExhausted(now)
	attempts := attemptsOn(incident, now)
	latest := attempts[len(attempts)-1]
	message := fmt.Sprintf(
		"Job monitor morning recovery exhausted\nFailed full scans: %d\nTimeline: %s\nProbable source: %s\nLatest error: %s\nNext automatic attempt: 18:00.\nAction now: none.",
		len(attempts), formatAttemptTimeline(attempts), latest.Provenance, compactText(latest.Error, 400),
	)
	if err := sendOperationalNotice(ctx, cfg, message); err != nil {
		return err
	}
	store.MarkMorningExhaustedNotification(now)
	return store.Save(cfg.StatePath)
}

func notifyDailySummary(ctx context.Context, cfg config.Config, store *state.Store, now time.Time) error {
	incident := store.Health.Incident
	if incident == nil || store.SummarySentOn(now) {
		return nil
	}
	attempts := attemptsOn(incident, now)
	if len(attempts) == 0 {
		attempts = incident.Attempts
	}
	latest := attempts[len(attempts)-1]
	diagnosis := diagnoseFailure(errors.New(latest.Error))
	message := fmt.Sprintf(
		"Job monitor daily recovery failed\nDate: %s\nFailed full scans: %d\nTimeline: %s\nProbable source: %s\nLatest error: %s\nLast successful scan: %s\nAutomatic retries are finished for today.\nRequired action: %s\nNext automatic scan: 11:00 tomorrow.",
		now.Format("02.01.06"), len(attempts), formatAttemptTimeline(attempts), latest.Provenance,
		compactText(latest.Error, 400), formatOptionalTime(store.Health.LastSuccessAt), diagnosis.Action,
	)
	if err := sendOperationalNotice(ctx, cfg, message); err != nil {
		return err
	}
	store.MarkSummaryNotification(now)
	return store.Save(cfg.StatePath)
}

func sendOperationalNotice(ctx context.Context, cfg config.Config, message string) error {
	sendAlert, closeSender, err := newAlertSender(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeSender()
	return sendAlertWithRetry(ctx, sendAlert, message, cfg.SendRetries, time.Duration(cfg.SendDelaySeconds)*time.Second)
}

func recoveryMessage(incident *state.Incident, now time.Time, newAlerts int) string {
	if incident == nil || len(incident.Attempts) == 0 {
		return "Job monitor recovered | No action required."
	}
	latest := incident.Attempts[len(incident.Attempts)-1]
	return fmt.Sprintf(
		"Job monitor recovered | Recovered: %s | Incident started: %s | Failed full scans: %d | Probable source: %s | New alerts: %d | No action required.",
		now.Format("02.01.06 15:04 MST"), incident.StartedAt.Format("02.01.06 15:04 MST"),
		len(incident.Attempts), latest.Provenance, newAlerts,
	)
}

func classifyRunWindow(now time.Time) runWindow {
	hour := now.Hour()
	if hour >= 10 && hour < 18 {
		return morningWindow
	}
	if hour >= 18 {
		return eveningWindow
	}
	return startupWindow
}

func initialPhase(window runWindow, previousMorningFailures int) string {
	switch window {
	case morningWindow:
		if previousMorningFailures > 0 {
			return fmt.Sprintf("morning recovery %d/3", previousMorningFailures)
		}
		return "morning scan"
	case eveningWindow:
		return "evening scan"
	default:
		return "startup scan"
	}
}

func eveningCutoff(now time.Time) time.Time {
	year, month, day := now.Date()
	return time.Date(year, month, day, 18, 0, 0, 0, now.Location())
}

func sameLocalDate(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.In(a.Location()).Date()
	return ay == by && am == bm && ad == bd
}

func morningFailuresOn(incident *state.Incident, date time.Time) int {
	count := 0
	if incident == nil {
		return count
	}
	for _, attempt := range incident.Attempts {
		if sameLocalDate(attempt.At, date) && strings.HasPrefix(attempt.Phase, "morning") {
			count++
		}
	}
	return count
}

func nextMorningRecoveryAt(incident *state.Incident, date time.Time) time.Time {
	latest := date
	if incident == nil {
		return latest.Add(morningRecoveryInterval)
	}
	for _, attempt := range incident.Attempts {
		if sameLocalDate(attempt.At, date) && strings.HasPrefix(attempt.Phase, "morning") {
			if attempt.At.After(latest) {
				latest = attempt.At
			}
		}
	}
	return latest.Add(morningRecoveryInterval)
}

func attemptsOn(incident *state.Incident, date time.Time) []state.IncidentAttempt {
	var attempts []state.IncidentAttempt
	if incident == nil {
		return attempts
	}
	for _, attempt := range incident.Attempts {
		if sameLocalDate(attempt.At, date) {
			attempts = append(attempts, attempt)
		}
	}
	return attempts
}

func formatAttemptTimeline(attempts []state.IncidentAttempt) string {
	parts := make([]string, 0, len(attempts))
	for _, attempt := range attempts {
		parts = append(parts, fmt.Sprintf("%s %s", attempt.At.Format("15:04"), attempt.Phase))
	}
	return strings.Join(parts, "; ")
}

func lastIncidentError(incident *state.Incident) error {
	if incident == nil || len(incident.Attempts) == 0 {
		return errors.New("morning recovery already exhausted")
	}
	return errors.New(incident.Attempts[len(incident.Attempts)-1].Error)
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return "none recorded"
	}
	return value.Format("02.01.06 15:04 MST")
}

func compactText(value string, maxRunes int) string {
	message := strings.Join(strings.Fields(value), " ")
	runes := []rune(message)
	if len(runes) <= maxRunes {
		return message
	}
	return string(runes[:maxRunes-3]) + "..."
}

func diagnoseFailure(err error) failureDiagnosis {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "no such host"), strings.Contains(message, "server misbehaving"), strings.Contains(message, "temporary failure in name resolution"):
		return failureDiagnosis{
			Source: "local DNS, internet connection, or ISP resolver",
			Action: "Check whether other websites open. If they also fail, reconnect or restart the router; if only Plexus fails, wait for Plexus recovery.",
		}
	case strings.Contains(message, "status 429"):
		return failureDiagnosis{
			Source: "Plexus rate limiting",
			Action: "Open Plexus once in a browser. If it works, leave the monitor stopped until the next schedule; repeated 429 errors require reducing checks or contacting Plexus.",
		}
	case containsHTTP5xx(message):
		return failureDiagnosis{
			Source: "Plexus website or hosting server",
			Action: "Check Plexus in a browser. If Plexus alone is unavailable, no local restart will fix it; wait or report the outage to Plexus.",
		}
	case strings.Contains(message, "connection reset"), strings.Contains(message, "connection refused"), strings.Contains(message, "unexpected eof"), strings.Contains(message, "tls handshake"):
		return failureDiagnosis{
			Source: "Plexus host, hosting proxy, or network path",
			Action: "Check Plexus and one unrelated website in a browser. If only Plexus fails, wait or report it to Plexus; if both fail, reconnect the Mac or router.",
		}
	case strings.Contains(message, "deadline exceeded"), strings.Contains(message, "client.timeout"), strings.Contains(message, "i/o timeout"), strings.Contains(message, "timed out"):
		return failureDiagnosis{
			Source: "Plexus website or network path timeout",
			Action: "Check Plexus and one unrelated website in a browser. If only Plexus is slow, wait for Plexus; if all sites are slow, investigate the local connection.",
		}
	case strings.Contains(message, "status 403"), strings.Contains(message, "status 401"):
		return failureDiagnosis{
			Source: "Plexus access rejection",
			Action: "Open Plexus in a browser. If it opens there but the monitor remains blocked, the scraper request needs adjustment.",
		}
	case strings.Contains(message, "parse"), strings.Contains(message, "selector"), strings.Contains(message, "page format"):
		return failureDiagnosis{
			Source: "Plexus page structure change",
			Action: "Open Plexus and confirm listings still exist, then update the scraper selectors.",
		}
	case strings.Contains(message, "telegram"), strings.Contains(message, "sendmessage"):
		return failureDiagnosis{
			Source: "Telegram delivery",
			Action: "Check Telegram bot access, chat membership, and the configured chat id and token.",
		}
	case strings.Contains(message, "permission denied"), strings.Contains(message, "read-only file system"):
		return failureDiagnosis{
			Source: "local installation or file permissions",
			Action: "Run scripts/status.sh and verify the project and data directory remain writable by your macOS user.",
		}
	default:
		return failureDiagnosis{
			Source: "Plexus website or network path",
			Action: "Run scripts/status.sh, then check whether Plexus and Telegram are reachable in a browser before restarting the LaunchAgent.",
		}
	}
}

func containsHTTP5xx(message string) bool {
	for status := 500; status <= 599; status++ {
		if strings.Contains(message, fmt.Sprintf("status %d", status)) {
			return true
		}
	}
	return false
}
