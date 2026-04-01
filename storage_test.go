package main

import (
	"os"
	"path/filepath"
	"testing"
)

func tempDB(t *testing.T) *Storage {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := NewStorage(path)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMeta(t *testing.T) {
	s := tempDB(t)

	// Initially empty
	m, err := s.GetAllMeta()
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Fatalf("expected empty meta, got %v", m)
	}

	// Set and get
	if err := s.SetMeta("title", "Test Incident"); err != nil {
		t.Fatal(err)
	}
	val, err := s.GetMeta("title")
	if err != nil {
		t.Fatal(err)
	}
	if val != "Test Incident" {
		t.Fatalf("expected 'Test Incident', got %q", val)
	}

	// Upsert
	if err := s.SetMeta("title", "Updated"); err != nil {
		t.Fatal(err)
	}
	val, err = s.GetMeta("title")
	if err != nil {
		t.Fatal(err)
	}
	if val != "Updated" {
		t.Fatalf("expected 'Updated', got %q", val)
	}

	// Non-existent key
	val, err = s.GetMeta("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if val != "" {
		t.Fatalf("expected empty, got %q", val)
	}
}

func TestEventCRUD(t *testing.T) {
	s := tempDB(t)

	// Create
	endTime := "2026-04-01T14:30:00+09:00"
	ev, err := s.CreateEvent("2026-04-01T14:00:00+09:00", &endTime, "First event", "SOC", "Asia/Tokyo", []string{"detection", "analysis"})
	if err != nil {
		t.Fatal(err)
	}
	if ev.ID != 1 {
		t.Fatalf("expected id 1, got %d", ev.ID)
	}
	if ev.Description != "First event" {
		t.Fatalf("expected 'First event', got %q", ev.Description)
	}
	// Stored as UTC
	expectedEnd := "2026-04-01T05:30:00Z"
	if ev.TimestampEnd == nil || *ev.TimestampEnd != expectedEnd {
		t.Fatalf("expected timestamp_end %q (UTC), got %v", expectedEnd, ev.TimestampEnd)
	}
	if ev.InputTZ != "Asia/Tokyo" {
		t.Fatalf("expected input_tz 'Asia/Tokyo', got %q", ev.InputTZ)
	}
	// Timestamp should be UTC
	if ev.Timestamp != "2026-04-01T05:00:00Z" {
		t.Fatalf("expected UTC timestamp, got %q", ev.Timestamp)
	}
	if len(ev.Tags) != 2 || ev.Tags[0] != "analysis" || ev.Tags[1] != "detection" {
		t.Fatalf("unexpected tags: %v", ev.Tags)
	}
	if len(ev.Images) != 0 {
		t.Fatalf("expected no images, got %d", len(ev.Images))
	}

	// Create second event (point-in-time, no end)
	_, err = s.CreateEvent("2026-04-01T15:00:00+09:00", nil, "Second event", "IT", "", []string{"containment"})
	if err != nil {
		t.Fatal(err)
	}

	// List
	events, err := s.ListEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	// Should be sorted by timestamp (stored as UTC)
	if events[0].Timestamp != "2026-04-01T05:00:00Z" {
		t.Fatalf("wrong sort order: first event timestamp = %s", events[0].Timestamp)
	}

	// Update
	updated, err := s.UpdateEvent(1, "2026-04-01T14:30:00+09:00", nil, "Updated event", "SOC Lead", "", []string{"detection"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Description != "Updated event" {
		t.Fatalf("expected 'Updated event', got %q", updated.Description)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "detection" {
		t.Fatalf("unexpected tags after update: %v", updated.Tags)
	}

	// Update non-existent
	_, err = s.UpdateEvent(999, "2026-04-01T14:00:00+09:00", nil, "x", "x", "", nil)
	if err == nil {
		t.Fatal("expected error for non-existent event")
	}

	// Delete
	if err := s.DeleteEvent(1); err != nil {
		t.Fatal(err)
	}
	events, err = s.ListEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event after delete, got %d", len(events))
	}

	// Delete non-existent
	if err := s.DeleteEvent(999); err == nil {
		t.Fatal("expected error for non-existent event")
	}
}

func TestTags(t *testing.T) {
	s := tempDB(t)

	// No tags initially
	tags, err := s.ListTags()
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Fatalf("expected no tags, got %v", tags)
	}

	// Create events with tags
	s.CreateEvent("2026-04-01T14:00:00+09:00", nil, "e1", "", "", []string{"alpha", "beta"})
	s.CreateEvent("2026-04-01T15:00:00+09:00", nil, "e2", "", "", []string{"beta", "gamma"})

	tags, err = s.ListTags()
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %v", tags)
	}
	// Sorted
	expected := []string{"alpha", "beta", "gamma"}
	for i, tag := range tags {
		if tag != expected[i] {
			t.Fatalf("expected tag %q at index %d, got %q", expected[i], i, tag)
		}
	}

	// Empty tag should be ignored
	s.CreateEvent("2026-04-01T16:00:00+09:00", nil, "e3", "", "", []string{""})
	tags, _ = s.ListTags()
	if len(tags) != 3 {
		t.Fatalf("empty tag should not appear, got %v", tags)
	}
}

func TestImageCRUD(t *testing.T) {
	s := tempDB(t)

	// Create event first
	ev, err := s.CreateEvent("2026-04-01T14:00:00+09:00", nil, "Test", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Upload image
	data := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header
	img, err := s.CreateImage(ev.ID, "test.png", "image/png", data)
	if err != nil {
		t.Fatal(err)
	}
	if img.Filename != "test.png" {
		t.Fatalf("expected 'test.png', got %q", img.Filename)
	}

	// Get image with data
	imgData, err := s.GetImage(img.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(imgData.Data) != 4 {
		t.Fatalf("expected 4 bytes, got %d", len(imgData.Data))
	}

	// Event should have image metadata
	ev2, _ := s.GetEvent(ev.ID)
	if len(ev2.Images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(ev2.Images))
	}

	// Delete image
	if err := s.DeleteImage(img.ID); err != nil {
		t.Fatal(err)
	}
	ev3, _ := s.GetEvent(ev.ID)
	if len(ev3.Images) != 0 {
		t.Fatalf("expected 0 images after delete, got %d", len(ev3.Images))
	}

	// Delete non-existent
	if err := s.DeleteImage(999); err == nil {
		t.Fatal("expected error for non-existent image")
	}
}

func TestCascadeDelete(t *testing.T) {
	s := tempDB(t)

	ev, _ := s.CreateEvent("2026-04-01T14:00:00+09:00", nil, "Test", "", "", []string{"tag1"})
	s.CreateImage(ev.ID, "img.png", "image/png", []byte{1, 2, 3})

	// Delete event should cascade to tags and images
	if err := s.DeleteEvent(ev.ID); err != nil {
		t.Fatal(err)
	}
	tags, _ := s.ListTags()
	if len(tags) != 0 {
		t.Fatalf("expected tags to be cascade-deleted, got %v", tags)
	}
	_, err := s.GetImage(1)
	if err == nil {
		t.Fatal("expected image to be cascade-deleted")
	}
}

func TestExportMarkdown(t *testing.T) {
	s := tempDB(t)

	s.SetMeta("title", "Test Incident")
	s.SetMeta("case_id", "INC-001")
	s.CreateEvent("2026-04-01T14:00:00+09:00", nil, "First event", "SOC", "", []string{"detection"})
	s.CreateEvent("2026-04-01T14:15:00+09:00", nil, "Second event", "Analyst", "", []string{"analysis"})

	md, err := s.ExportMarkdown()
	if err != nil {
		t.Fatal(err)
	}

	// Check header
	if !contains(md, "[INC-001]") {
		t.Fatal("expected case ID in markdown")
	}
	if !contains(md, "Test Incident") {
		t.Fatal("expected title in markdown")
	}
	if !contains(md, "First event") {
		t.Fatal("expected first event description")
	}
	if !contains(md, "detection") {
		t.Fatal("expected tag in markdown")
	}
	if !contains(md, "+15m") {
		t.Fatal("expected time delta in markdown")
	}
}

func TestNewStorageCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "test.db")
	// Parent dir doesn't exist, should fail
	_, err := NewStorage(path)
	if err == nil {
		t.Fatal("expected error for non-existent parent dir")
	}

	// With existing parent dir
	path = filepath.Join(dir, "test.db")
	s, err := NewStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected DB file to be created")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchStr(s, sub)
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
