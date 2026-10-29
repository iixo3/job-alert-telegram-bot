package jobs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type ClientConfig struct {
	SourceURL    string
	MaxPages     int
	HTTPTimeout  time.Duration
	UserAgent    string
	PageAttempts int
	RetryDelay   time.Duration
}

type Client struct {
	httpClient   *http.Client
	sourceURL    string
	maxPages     int
	userAgent    string
	pageAttempts int
	retryDelay   time.Duration
}

func NewClient(cfg ClientConfig) *Client {
	timeout := cfg.HTTPTimeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	maxPages := cfg.MaxPages
	if maxPages <= 0 {
		maxPages = 8
	}
	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = "job-alert/0.1"
	}
	pageAttempts := cfg.PageAttempts
	if pageAttempts <= 0 {
		pageAttempts = 3
	}
	retryDelay := cfg.RetryDelay
	if retryDelay <= 0 {
		retryDelay = 2 * time.Second
	}
	return &Client{
		httpClient:   &http.Client{Timeout: timeout},
		sourceURL:    cfg.SourceURL,
		maxPages:     maxPages,
		userAgent:    userAgent,
		pageAttempts: pageAttempts,
		retryDelay:   retryDelay,
	}
}

func (c *Client) FetchAll(ctx context.Context) ([]Job, error) {
	nextURL := c.sourceURL
	seenURLs := map[string]bool{}
	var out []Job

	for pageNum := 1; pageNum <= c.maxPages && nextURL != ""; pageNum++ {
		jobs, next, err := c.fetchPage(ctx, nextURL)
		if err != nil {
			return nil, err
		}
		for _, job := range jobs {
			if job.URL == "" || seenURLs[job.URL] {
				continue
			}
			seenURLs[job.URL] = true
			out = append(out, job)
		}
		nextURL = next
	}

	return out, nil
}

func (c *Client) fetchPage(ctx context.Context, pageURL string) ([]Job, string, error) {
	var lastErr error
	for attempt := 1; attempt <= c.pageAttempts; attempt++ {
		jobs, next, retryable, err := c.fetchPageOnce(ctx, pageURL)
		if err == nil {
			return jobs, next, nil
		}
		lastErr = err
		if !retryable || attempt == c.pageAttempts {
			break
		}
		if err := waitForRetry(ctx, time.Duration(attempt)*c.retryDelay); err != nil {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("fetch %s failed after %d attempts: %w", pageURL, c.pageAttempts, lastErr)
}

func (c *Client) fetchPageOnce(ctx context.Context, pageURL string) ([]Job, string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, "", false, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", ctx.Err() == nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		retryable := resp.StatusCode == http.StatusRequestTimeout ||
			resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode >= 500
		return nil, "", retryable, fmt.Errorf("fetch %s: status %d", pageURL, resp.StatusCode)
	}

	jobs, next, err := ParseJobsPage(resp.Body, resp.Request.URL)
	return jobs, next, false, err
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func ParseJobsPage(r io.Reader, base *url.URL) ([]Job, string, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, "", err
	}

	var parsed []Job
	doc.Find("li[role='article']").Each(func(_ int, card *goquery.Selection) {
		job := Job{
			Title:       cleanText(card.Find("h2").First().Text()),
			Description: cleanText(card.Find("p").First().Text()),
		}

		items := make([]string, 0, 4)
		card.Find("ul li").Each(func(_ int, li *goquery.Selection) {
			text := cleanText(li.Text())
			if text != "" {
				items = append(items, text)
			}
		})
		if len(items) > 0 {
			job.Salary = items[0]
		}
		for _, item := range items[1:] {
			lower := strings.ToLower(item)
			if strings.HasPrefix(lower, "posted:") {
				job.Posted = cleanText(strings.TrimSpace(strings.TrimPrefix(item, "Posted:")))
				continue
			}
			if job.WorkMode == "" {
				job.WorkMode = item
				continue
			}
			if job.Region == "" {
				job.Region = item
				continue
			}
			job.Region = cleanText(job.Region + " " + item)
		}

		card.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			if job.URL != "" {
				return
			}
			href, ok := a.Attr("href")
			if !ok {
				return
			}
			if !strings.Contains(href, "/jobs/") {
				return
			}
			job.URL = resolveURL(base, href)
			job.ID = jobID(job.URL)
		})

		if job.Title != "" && job.URL != "" {
			parsed = append(parsed, job)
		}
	})

	nextURL := ""
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		if nextURL != "" {
			return
		}
		if cleanText(a.Text()) != "Next Page" {
			return
		}
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		nextURL = resolveURL(base, href)
	})

	return parsed, nextURL, nil
}

func cleanText(input string) string {
	return strings.Join(strings.Fields(input), " ")
}

func resolveURL(base *url.URL, raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if base == nil {
		return parsed.String()
	}
	return base.ResolveReference(parsed).String()
}

func jobID(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	slug := path.Base(strings.TrimSuffix(parsed.Path, "/"))
	if slug == "." || slug == "/" || slug == "" {
		return rawURL
	}
	return slug
}
