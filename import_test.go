package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportJSON(t *testing.T) {
	s := tempDB(t)

	data := `[
		{"timestamp":"2026-04-01T14:00:00+09:00","description":"Event 1","actor":"SOC","tags":["detection"]},
		{"timestamp":"2026-04-01T15:00:00+09:00","timestamp_end":"2026-04-01T15:30:00+09:00","description":"Event 2","actor":"Analyst","tags":["analysis","containment"]}
	]`
	path := filepath.Join(t.TempDir(), "events.json")
	os.WriteFile(path, []byte(data), 0644)

	n, err := importFile(s, path, "")
	if err != nil {
		t.Fatalf("importFile: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 imported, got %d", n)
	}

	events, _ := s.ListEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 events in DB, got %d", len(events))
	}
	if events[0].Description != "Event 1" {
		t.Fatalf("unexpected description: %q", events[0].Description)
	}
	if events[1].TimestampEnd == nil || *events[1].TimestampEnd != "2026-04-01T06:30:00Z" {
		t.Fatalf("unexpected timestamp_end: %v", events[1].TimestampEnd)
	}
	if len(events[1].Tags) != 2 {
		t.Fatalf("expected 2 tags, got %v", events[1].Tags)
	}
}

func TestImportCSV(t *testing.T) {
	s := tempDB(t)

	data := `timestamp,timestamp_end,description,actor,tags
2026-04-01T14:00:00+09:00,,Phishing reported,SOC,"detection,analysis"
2026-04-01T15:00:00+09:00,2026-04-01T16:00:00+09:00,Analysis complete,Analyst,analysis
2026-04-01T17:00:00+09:00,,Contained,IT,containment
`
	path := filepath.Join(t.TempDir(), "events.csv")
	os.WriteFile(path, []byte(data), 0644)

	n, err := importFile(s, path, "")
	if err != nil {
		t.Fatalf("importFile: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 imported, got %d", n)
	}

	events, _ := s.ListEvents()
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	// First event: no end time, 2 tags
	if events[0].TimestampEnd != nil {
		t.Fatalf("expected nil timestamp_end, got %v", events[0].TimestampEnd)
	}
	if len(events[0].Tags) != 2 {
		t.Fatalf("expected 2 tags for row 1, got %v", events[0].Tags)
	}
	// Second event: has end time
	if events[1].TimestampEnd == nil || *events[1].TimestampEnd != "2026-04-01T07:00:00Z" {
		t.Fatalf("unexpected timestamp_end: %v", events[1].TimestampEnd)
	}
}

func TestImportCSVMinimalColumns(t *testing.T) {
	s := tempDB(t)

	// Only timestamp and description columns
	data := `timestamp,description
2026-04-01T14:00:00+09:00,Something happened
2026-04-01T15:00:00+09:00,Another thing
`
	path := filepath.Join(t.TempDir(), "events.csv")
	os.WriteFile(path, []byte(data), 0644)

	n, err := importFile(s, path, "")
	if err != nil {
		t.Fatalf("importFile: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2, got %d", n)
	}
	events, _ := s.ListEvents()
	if events[0].Actor != "" {
		t.Fatalf("expected empty actor, got %q", events[0].Actor)
	}
}

func TestImportJSONMissingTimestamp(t *testing.T) {
	s := tempDB(t)

	data := `[{"description":"no timestamp"}]`
	path := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(path, []byte(data), 0644)

	_, err := importFile(s, path, "json")
	if err == nil {
		t.Fatal("expected error for missing timestamp")
	}
}

func TestImportCSVMissingHeader(t *testing.T) {
	s := tempDB(t)

	data := `description,actor
Something,SOC
`
	path := filepath.Join(t.TempDir(), "bad.csv")
	os.WriteFile(path, []byte(data), 0644)

	_, err := importFile(s, path, "csv")
	if err == nil {
		t.Fatal("expected error for missing timestamp column")
	}
}

func TestImportFormatDetection(t *testing.T) {
	s := tempDB(t)

	jsonData := `[{"timestamp":"2026-04-01T14:00:00+09:00","description":"test"}]`
	jsonPath := filepath.Join(t.TempDir(), "data.json")
	os.WriteFile(jsonPath, []byte(jsonData), 0644)

	n, err := importFile(s, jsonPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}

	csvData := "timestamp,description\n2026-04-01T15:00:00+09:00,csv test\n"
	csvPath := filepath.Join(t.TempDir(), "data.csv")
	os.WriteFile(csvPath, []byte(csvData), 0644)

	n, err = importFile(s, csvPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}
}

func TestImportUnknownFormat(t *testing.T) {
	s := tempDB(t)
	path := filepath.Join(t.TempDir(), "data.txt")
	os.WriteFile(path, []byte("hello"), 0644)

	_, err := importFile(s, path, "xml")
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}
