# Job Alert Telegram Bot

A small Go program that checks the public job board at [plexusrs.com/jobs](https://plexusrs.com/jobs/) twice a day, keeps the listings that pass your filters, and sends each new one to a Telegram group or channel through a Telegram bot. It runs on a Mac, scheduled by macOS `launchd`; no server is needed.

## What it does

- Reads every listing page of the job board (up to `max_pages`).
- Drops a listing if its title (or the last part of its link) contains a word from `title_denylist`, or its location contains a word from `location_denylist`. Matching ignores upper and lower case. Both lists are empty by default, so every listing passes until you fill them.
- Sends each new match as one message, oldest first:
  `Position | Location | Salary | Posted: <date> | <link>`
- Remembers what it has sent, so each listing is sent once. The first live run sends all current matches.
- Sends a short "healthy" message once a week, and failure notices when scans fail (see below). Other errors are reported at most once a day.

## How it works

```text
job board pages -> page reader -> filters -> sent list -> Telegram Bot API -> your group or channel
```

| Folder | Role |
|---|---|
| `cmd/job-alert` | Commands (`dry-run`, `run`, `telegram-chats`, `send-test`) and the failure-recovery logic |
| `internal/jobs` | Page reader (HTML parsed with goquery), filters, message format |
| `internal/state` | Sent list and health record, kept in one JSON file (`data/seen_jobs.json`) |
| `internal/notifier` | Telegram sender. Telegram is the only sender in use; earlier WhatsApp and Hermes senders remain in the code, unused |
| `internal/config` | Settings loader (`config.json`) |
| `scripts/`, `launchd/` | macOS schedule, status check and Telegram setup helpers |

The page reader is written for this job board's page layout. If the board changes its pages, the reader needs updating; the failure notice says so ("page structure change").

### When a scan fails

- Each page gets up to three quick attempts on network errors, timeouts (HTTP 408), rate limits (HTTP 429) and server errors.
- When a problem starts, Telegram receives one "issue detected" notice: the likely cause, the error and what the program will do next.
- If the 11:00 scan still fails, the program runs up to three more full scans, 15 minutes apart. If they all fail, it sends a "morning recovery exhausted" notice and waits for the 18:00 scan.
- If 18:00 fails as well, Telegram receives a daily summary: the attempts and their times, the likely cause, the latest error, the last successful scan and what to check.
- The incident is saved with the sent list, so a restart doesn't reset the retry count. There is no endless retry loop.
- The next successful scan sends any missed listings, oldest first, and one recovery message.

## Run it

Needs Go 1.25 or later. The schedule and helper scripts are for macOS; `scripts/status.sh` uses `jq` and `scripts/set_telegram_chat_id.sh` uses Node.js. Run every command from the project folder.

1. Create your settings file (it is ignored by git, so your bot key stays local):

   ```sh
   cp config.example.json config.json
   ```

2. Add filters if you want them, for example:

   ```json
   "title_denylist": ["intern"],
   "location_denylist": ["on-site"]
   ```

3. Check what would be sent. A dry run sends nothing and saves nothing:

   ```sh
   go run ./cmd/job-alert dry-run --config config.json
   ```

4. Create a bot with Telegram's @BotFather and put its key in `telegram_bot_token`. Add the bot to a group or channel, send one message there, then list the chats the bot can see:

   ```sh
   go run ./cmd/job-alert telegram-chats --config config.json
   ```

   Put the chat's ID in `telegram_chat_id` (or run `scripts/set_telegram_chat_id.sh '<chat-id>'`), then send a test message:

   ```sh
   go run ./cmd/job-alert send-test --config config.json
   ```

5. Build, then install the schedule:

   ```sh
   make build
   scripts/install_launch_agent.sh
   ```

   It runs at login and every day at 11:00 and 18:00 local time. The Mac must be awake and online; checks missed during sleep run once when it wakes.

Other commands:

| Command | What it does |
|---|---|
| `./bin/job-alert run --config config.json` | One live scan now: sends new matches only |
| `./bin/job-alert run --force --config config.json` | One scan now, even after the day's automatic retries are used up |
| `scripts/status.sh` | Schedule state, last success, last error, open incident |
| `make test` | Runs the tests |

## Credits and use

Built by iixo3. The code is published to show the work: you're welcome to read it, but it is not licensed for reuse.
