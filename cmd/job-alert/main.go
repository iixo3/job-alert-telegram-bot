package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"job-alert/internal/config"
	"job-alert/internal/jobs"
	"job-alert/internal/notifier"
	"job-alert/internal/state"
)

const (
	healthyNotificationInterval = 7 * 24 * time.Hour
	failureNotificationCooldown = 24 * time.Hour
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		usage()
		return fmt.Errorf("missing command")
	}

	command := os.Args[1]
	fs := flag.NewFlagSet(command, flag.ExitOnError)
	configPath := fs.String("config", "config.json", "config file path")
	force := fs.Bool("force", false, "force one immediate scan outside the automatic recovery budget")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	timeout := time.Duration(cfg.HTTPTimeoutSeconds+45) * time.Second
	switch command {
	case "run":
		timeout = 90 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	switch command {
	case "dry-run":
		return dryRun(ctx, cfg)
	case "run":
		return liveRun(ctx, cfg, *force)
	case "telegram-chats":
		return telegramChats(ctx, cfg)
	case "send-test":
		return sendTest(ctx, cfg)
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func dryRun(ctx context.Context, cfg config.Config) error {
	matches, _, err := fetchMatches(ctx, cfg)
	if err != nil {
		return err
	}
	fmt.Printf("matched jobs: %d\n\n", len(matches))
	for _, job := range matches {
		fmt.Println(jobs.FormatMessage(job))
	}
	return nil
}

func liveRun(ctx context.Context, cfg config.Config, force bool) error {
	store, err := state.Load(cfg.StatePath)
	if err != nil {
		return err
	}
	now := time.Now()
	store.BeginRun(now)
	if err := store.Save(cfg.StatePath); err != nil {
		return err
	}

	matches, allJobs, err := fetchMatchesWithRecovery(ctx, cfg, store, force)
	if err != nil {
		return err
	}
	recoveredIncident := store.Health.Incident

	unseen := make([]jobs.Job, 0, len(matches))
	for _, job := range matches {
		if !store.Seen(job.URL) {
			unseen = append(unseen, job)
		}
	}
	jobs.SortOldestFirst(unseen)

	recoveryDue := recoveredIncident != nil
	heartbeatDue := !recoveryDue && store.HeartbeatDue(now, healthyNotificationInterval)
	fmt.Printf("fetched jobs: %d, matched: %d, unseen matched: %d\n", len(allJobs), len(matches), len(unseen))

	var sendAlert func(context.Context, string) error
	closeSender := func() {}
	if len(unseen) > 0 || heartbeatDue || recoveryDue {
		sendAlert, closeSender, err = newAlertSender(ctx, cfg)
		if err != nil {
			return failLiveRun(ctx, cfg, store, err)
		}
	}
	defer closeSender()

	sendDelay := time.Duration(cfg.SendDelaySeconds) * time.Second
	for i, job := range unseen {
		msg := jobs.FormatMessage(job)
		if err := sendAlertWithRetry(ctx, sendAlert, msg, cfg.SendRetries, sendDelay); err != nil {
			return failLiveRun(ctx, cfg, store, fmt.Errorf("send %q: %w", job.Title, err))
		}
		store.Mark(job, time.Now())
		if err := store.Save(cfg.StatePath); err != nil {
			return failLiveRun(ctx, cfg, store, err)
		}
		fmt.Printf("sent: %s\n", job.Title)
		if i < len(unseen)-1 && sendDelay > 0 {
			if err := sleepContext(ctx, sendDelay); err != nil {
				return failLiveRun(ctx, cfg, store, err)
			}
		}
	}

	if recoveryDue {
		message := recoveryMessage(recoveredIncident, time.Now(), len(unseen))
		if err := sendAlertWithRetry(ctx, sendAlert, message, cfg.SendRetries, sendDelay); err != nil {
			return failLiveRun(ctx, cfg, store, fmt.Errorf("send recovery notification: %w", err))
		}
		store.MarkHeartbeat(time.Now())
		fmt.Println("sent: recovery notification")
	} else if heartbeatDue {
		message := fmt.Sprintf(
			"Job monitor healthy | Scanned: %d | Matched: %d | New alerts: %d | Checked: %s",
			len(allJobs), len(matches), len(unseen), now.Format("02.01.06 15:04 MST"),
		)
		if err := sendAlertWithRetry(ctx, sendAlert, message, cfg.SendRetries, sendDelay); err != nil {
			return failLiveRun(ctx, cfg, store, fmt.Errorf("send weekly heartbeat: %w", err))
		}
		store.MarkHeartbeat(time.Now())
		fmt.Println("sent: weekly heartbeat")
	}

	store.ClearIncident()
	store.RecordSuccess(time.Now())
	return store.Save(cfg.StatePath)
}

func failLiveRun(ctx context.Context, cfg config.Config, store *state.Store, runErr error) error {
	now := time.Now()
	store.RecordFailure(now, runErr)
	saveErr := store.Save(cfg.StatePath)

	var notifyErr error
	if store.FailureNotificationDue(now, failureNotificationCooldown) {
		sendAlert, closeSender, err := newAlertSender(ctx, cfg)
		if err != nil {
			notifyErr = err
		} else {
			defer closeSender()
			message := fmt.Sprintf(
				"Job monitor failed | Checked: %s | Error: %s",
				now.Format("02.01.06 15:04 MST"), compactError(runErr, 500),
			)
			if err := sendAlertWithRetry(ctx, sendAlert, message, 1, 0); err != nil {
				notifyErr = err
			} else {
				store.MarkFailureNotification(now)
				if err := store.Save(cfg.StatePath); err != nil {
					saveErr = errors.Join(saveErr, err)
				}
			}
		}
	}

	return errors.Join(
		runErr,
		wrapOptionalError("save health state", saveErr),
		wrapOptionalError("send failure notification", notifyErr),
	)
}

func compactError(err error, maxRunes int) string {
	message := strings.Join(strings.Fields(err.Error()), " ")
	runes := []rune(message)
	if len(runes) <= maxRunes {
		return message
	}
	return string(runes[:maxRunes-3]) + "..."
}

func wrapOptionalError(context string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", context, err)
}

func sendAlertWithRetry(ctx context.Context, sendAlert func(context.Context, string) error, msg string, attempts int, delay time.Duration) error {
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := sendAlert(ctx, msg); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt < attempts && delay > 0 {
			wait := time.Duration(attempt) * delay
			if err := sleepContext(ctx, wait); err != nil {
				return fmt.Errorf("wait before retry: %w; last send error: %v", err, lastErr)
			}
		}
	}
	return lastErr
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newAlertSender(ctx context.Context, cfg config.Config) (func(context.Context, string) error, func(), error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Notifier)) {
	case "", "telegram_bot":
		client := notifier.NewTelegramBot(cfg.TelegramBotToken, cfg.TelegramChatID, time.Duration(cfg.SendTimeoutSeconds)*time.Second)
		return func(ctx context.Context, msg string) error {
			return client.SendText(ctx, msg)
		}, func() {}, nil
	default:
		return nil, nil, fmt.Errorf("unknown notifier %q", cfg.Notifier)
	}
}

func telegramChats(ctx context.Context, cfg config.Config) error {
	client := notifier.NewTelegramBot(cfg.TelegramBotToken, cfg.TelegramChatID, time.Duration(cfg.SendTimeoutSeconds)*time.Second)
	chats, err := client.RecentChats(ctx)
	if err != nil {
		return err
	}
	if len(chats) == 0 {
		fmt.Println("no recent chats found; add the bot to your group/channel and send a message there")
		return nil
	}
	for _, chat := range chats {
		fmt.Printf("%s\t%s\t%s\n", chat.ID, chat.Type, chat.Name)
	}
	return nil
}

func sendTest(ctx context.Context, cfg config.Config) error {
	sendAlert, closeSender, err := newAlertSender(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeSender()
	return sendAlert(ctx, "Job alert test from direct Telegram bot")
}

func fetchMatches(ctx context.Context, cfg config.Config) ([]jobs.Job, []jobs.Job, error) {
	client := jobs.NewClient(jobs.ClientConfig{
		SourceURL:   cfg.SourceURL,
		MaxPages:    cfg.MaxPages,
		UserAgent:   cfg.UserAgent,
		HTTPTimeout: time.Duration(cfg.HTTPTimeoutSeconds) * time.Second,
	})

	allJobs, err := client.FetchAll(ctx)
	if err != nil {
		return nil, nil, err
	}

	matches := jobs.Filter(allJobs, jobs.Criteria{
		TitleDenylist:    cfg.TitleDenylist,
		LocationDenylist: cfg.LocationDenylist,
	})
	return matches, allJobs, nil
}

func usage() {
	bin := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, `usage:
  %s dry-run --config config.json
  %s telegram-chats --config config.json
  %s send-test --config config.json
  %s run [--force] --config config.json
`, bin, bin, bin, bin)
}
