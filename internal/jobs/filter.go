package jobs

import "strings"

type Criteria struct {
	TitleDenylist    []string
	LocationDenylist []string
}

func Filter(input []Job, criteria Criteria) []Job {
	output := make([]Job, 0, len(input))
	for _, job := range input {
		if ExcludedByTitle(jobTitleSearchText(job), criteria.TitleDenylist) {
			continue
		}
		if ExcludedByLocation(job.Location(), criteria.LocationDenylist) {
			continue
		}
		output = append(output, job)
	}
	return output
}

func jobTitleSearchText(job Job) string {
	slugText := strings.ReplaceAll(job.ID, "-", " ")
	return strings.TrimSpace(job.Title + " " + slugText)
}

func ExcludedByTitle(title string, denylist []string) bool {
	return containsAny(title, denylist)
}

func ExcludedByLocation(location string, denylist []string) bool {
	return containsAny(location, denylist)
}

func containsAny(value string, terms []string) bool {
	value = strings.ToLower(value)
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}
