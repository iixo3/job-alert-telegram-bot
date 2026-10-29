package jobs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseJobsPage(t *testing.T) {
	base, err := url.Parse("https://plexusrs.com/jobs/")
	if err != nil {
		t.Fatal(err)
	}

	html := `
<ul>
  <li role="article">
    <h2>Growth Lead</h2>
    <ul>
      <li>$140k-$180k</li>
      <li>Remote</li>
      <li>Global</li>
      <li>Posted: 11.06.26</li>
    </ul>
    <p>Growth role description.</p>
    <a href="/jobs/growth-lead-1/">View & Apply</a>
  </li>
</ul>
<a href="https://plexusrs.com/jobs/page/2/">Next Page</a>
`

	got, next, err := ParseJobsPage(strings.NewReader(html), base)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 job, got %d", len(got))
	}
	job := got[0]
	if job.Title != "Growth Lead" {
		t.Fatalf("title = %q", job.Title)
	}
	if job.Salary != "$140k-$180k" {
		t.Fatalf("salary = %q", job.Salary)
	}
	if job.Location() != "Remote - Global" {
		t.Fatalf("location = %q", job.Location())
	}
	if job.Posted != "11.06.26" {
		t.Fatalf("posted = %q", job.Posted)
	}
	if job.URL != "https://plexusrs.com/jobs/growth-lead-1/" {
		t.Fatalf("url = %q", job.URL)
	}
	if next != "https://plexusrs.com/jobs/page/2/" {
		t.Fatalf("next = %q", next)
	}
}

func TestSortOldestFirst(t *testing.T) {
	input := []Job{
		{Title: "Newest", Posted: "09.09.26"},
		{Title: "Unknown first", Posted: "N/A"},
		{Title: "Oldest", Posted: "20.07.26"},
		{Title: "Same day first", Posted: "01.09.26"},
		{Title: "Same day second", Posted: "01.09.26"},
		{Title: "Unknown second", Posted: ""},
	}

	SortOldestFirst(input)

	got := make([]string, 0, len(input))
	for _, job := range input {
		got = append(got, job.Title)
	}
	want := []string{
		"Oldest",
		"Same day first",
		"Same day second",
		"Newest",
		"Unknown first",
		"Unknown second",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %#v, want %#v", got, want)
	}
}

func TestClientRetriesTransientPageFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) < 3 {
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`
<ul>
  <li role="article">
    <h2>Growth Lead</h2>
    <ul><li>$140k</li><li>Remote</li><li>Region A</li><li>Posted: 10.10.26</li></ul>
    <a href="/jobs/growth-lead-1/">View & Apply</a>
  </li>
</ul>`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		SourceURL:    server.URL,
		MaxPages:     1,
		HTTPTimeout:  time.Second,
		PageAttempts: 3,
		RetryDelay:   time.Millisecond,
	})
	got, err := client.FetchAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 3 {
		t.Fatalf("requests = %d, want 3", requests.Load())
	}
	if len(got) != 1 || got[0].Title != "Growth Lead" {
		t.Fatalf("jobs = %#v", got)
	}
}

func TestClientDoesNotRetryPermanentHTTPFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		SourceURL:    server.URL,
		MaxPages:     1,
		HTTPTimeout:  time.Second,
		PageAttempts: 3,
		RetryDelay:   time.Millisecond,
	})
	_, err := client.FetchAll(context.Background())
	if err == nil {
		t.Fatal("expected fetch error")
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}
