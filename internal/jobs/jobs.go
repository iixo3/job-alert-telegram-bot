package jobs

import (
	"sort"
	"strings"
	"time"
)

type Job struct {
	ID          string
	Title       string
	Salary      string
	WorkMode    string
	Region      string
	Posted      string
	Description string
	URL         string
}

func (j Job) Location() string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(j.WorkMode) != "" {
		parts = append(parts, strings.TrimSpace(j.WorkMode))
	}
	if strings.TrimSpace(j.Region) != "" {
		parts = append(parts, strings.TrimSpace(j.Region))
	}
	if len(parts) == 0 {
		return "N/A"
	}
	return strings.Join(parts, " - ")
}

func FormatMessage(job Job) string {
	title := fallback(job.Title)
	location := fallback(job.Location())
	salary := fallback(job.Salary)
	posted := fallback(job.Posted)
	return strings.Join([]string{
		title,
		location,
		salary,
		"Posted: " + posted,
		job.URL,
	}, " | ")
}

func SortOldestFirst(input []Job) {
	sort.SliceStable(input, func(i, j int) bool {
		left, leftOK := postedDate(input[i].Posted)
		right, rightOK := postedDate(input[j].Posted)
		switch {
		case leftOK && rightOK:
			return left.Before(right)
		case leftOK:
			return true
		default:
			return false
		}
	})
}

func postedDate(value string) (time.Time, bool) {
	parsed, err := time.Parse("02.01.06", strings.TrimSpace(value))
	return parsed, err == nil
}

func fallback(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "N/A"
	}
	return value
}
