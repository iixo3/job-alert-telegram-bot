package jobs

import "testing"

func TestFilter(t *testing.T) {
	criteria := Criteria{
		TitleDenylist: []string{
			"intern",
			"junior",
		},
		LocationDenylist: []string{"on-site"},
	}

	input := []Job{
		{Title: "Growth Lead", WorkMode: "Remote", Region: "Global", URL: "1"},
		{Title: "Marketing Intern", WorkMode: "Remote", Region: "Global", URL: "2"},
		{Title: "JUNIOR Analyst", WorkMode: "Remote", Region: "Region A", URL: "3"},
		{Title: "BD Lead", WorkMode: "On-site", Region: "Region A", URL: "4"},
		{Title: "Sales Director", WorkMode: "ON-SITE", Region: "Region B", URL: "5"},
		{Title: "Operations Assistant...", WorkMode: "Remote", Region: "Region B", ID: "operations-assistant-intern-4686", URL: "6"},
	}

	got := Filter(input, criteria)
	if len(got) != 1 {
		t.Fatalf("expected 1 job, got %d: %#v", len(got), got)
	}
	if got[0].Title != "Growth Lead" {
		t.Fatalf("unexpected match: %s", got[0].Title)
	}
}
