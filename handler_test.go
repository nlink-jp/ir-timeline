package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func testServer(t *testing.T) (*Storage, *httptest.Server) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	store, err := NewStorage(path)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Minimal embedded FS for testing
	webFS := fstest.MapFS{
		"web/index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}
	h := NewHandler(store, webFS)
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return store, ts
}

func TestGetMetaEmpty(t *testing.T) {
	_, ts := testServer(t)
	resp, err := http.Get(ts.URL + "/api/meta")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var m map[string]string
	json.NewDecoder(resp.Body).Decode(&m)
	if len(m) != 0 {
		t.Fatalf("expected empty meta, got %v", m)
	}
}

func TestPutGetMeta(t *testing.T) {
	_, ts := testServer(t)

	body := `{"title":"Test","case_id":"INC-001"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/meta", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("PUT expected 200, got %d", resp.StatusCode)
	}

	resp, _ = http.Get(ts.URL + "/api/meta")
	defer resp.Body.Close()
	var m map[string]string
	json.NewDecoder(resp.Body).Decode(&m)
	if m["title"] != "Test" || m["case_id"] != "INC-001" {
		t.Fatalf("unexpected meta: %v", m)
	}
}

func TestEventLifecycle(t *testing.T) {
	_, ts := testServer(t)

	// Create
	body := `{"timestamp":"2026-04-01T14:00:00+09:00","description":"Test event","actor":"SOC","tags":["detection"]}`
	resp, err := http.Post(ts.URL+"/api/events", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("POST expected 201, got %d", resp.StatusCode)
	}

	// List
	resp, _ = http.Get(ts.URL + "/api/events")
	var events []Event
	json.NewDecoder(resp.Body).Decode(&events)
	resp.Body.Close()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Description != "Test event" {
		t.Fatalf("unexpected description: %q", events[0].Description)
	}

	// Update
	body2 := `{"timestamp":"2026-04-01T14:30:00+09:00","description":"Updated","actor":"Lead","tags":["analysis"]}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/events/1", bytes.NewBufferString(body2))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	var updated Event
	json.NewDecoder(resp.Body).Decode(&updated)
	resp.Body.Close()
	if updated.Description != "Updated" {
		t.Fatalf("expected 'Updated', got %q", updated.Description)
	}

	// Delete
	req, _ = http.NewRequest("DELETE", ts.URL+"/api/events/1", nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("DELETE expected 200, got %d", resp.StatusCode)
	}

	// Verify deleted
	resp, _ = http.Get(ts.URL + "/api/events")
	json.NewDecoder(resp.Body).Decode(&events)
	resp.Body.Close()
	if len(events) != 0 {
		t.Fatalf("expected 0 events after delete, got %d", len(events))
	}
}

func TestEventValidation(t *testing.T) {
	_, ts := testServer(t)

	// Missing timestamp
	body := `{"description":"No timestamp"}`
	resp, _ := http.Post(ts.URL+"/api/events", "application/json", bytes.NewBufferString(body))
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for missing timestamp, got %d", resp.StatusCode)
	}

	// Invalid JSON
	resp, _ = http.Post(ts.URL+"/api/events", "application/json", bytes.NewBufferString("{bad"))
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for invalid JSON, got %d", resp.StatusCode)
	}

	// Update non-existent
	req, _ := http.NewRequest("PUT", ts.URL+"/api/events/999", bytes.NewBufferString(`{"timestamp":"2026-04-01T14:00:00+09:00"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 for non-existent event, got %d", resp.StatusCode)
	}

	// Delete non-existent
	req, _ = http.NewRequest("DELETE", ts.URL+"/api/events/999", nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 for non-existent event, got %d", resp.StatusCode)
	}
}

func TestImageUploadAndServe(t *testing.T) {
	_, ts := testServer(t)

	// Create event first
	body := `{"timestamp":"2026-04-01T14:00:00+09:00","description":"Test","tags":[]}`
	resp, _ := http.Post(ts.URL+"/api/events", "application/json", bytes.NewBufferString(body))
	resp.Body.Close()

	// Upload image via multipart
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("file", "test.png")
	part.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) // PNG header
	w.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/api/events/1/images", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, _ = http.DefaultClient.Do(req)
	var img Image
	json.NewDecoder(resp.Body).Decode(&img)
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("expected 201 for image upload, got %d", resp.StatusCode)
	}
	if img.Filename != "test.png" {
		t.Fatalf("expected 'test.png', got %q", img.Filename)
	}

	// Serve image
	resp, _ = http.Get(ts.URL + "/api/images/1")
	imgBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 for image serve, got %d", resp.StatusCode)
	}
	if len(imgBytes) != 8 {
		t.Fatalf("expected 8 bytes, got %d", len(imgBytes))
	}
	if resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("expected image/png, got %s", resp.Header.Get("Content-Type"))
	}

	// Delete image
	req, _ = http.NewRequest("DELETE", ts.URL+"/api/images/1", nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 for image delete, got %d", resp.StatusCode)
	}

	// Non-existent image
	resp, _ = http.Get(ts.URL + "/api/images/999")
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 for non-existent image, got %d", resp.StatusCode)
	}
}

func TestImageUploadToNonExistentEvent(t *testing.T) {
	_, ts := testServer(t)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("file", "test.png")
	part.Write([]byte{0x89, 0x50})
	w.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/api/events/999/images", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestTagsList(t *testing.T) {
	_, ts := testServer(t)

	// No tags
	resp, _ := http.Get(ts.URL + "/api/tags")
	var tags []string
	json.NewDecoder(resp.Body).Decode(&tags)
	resp.Body.Close()
	if len(tags) != 0 {
		t.Fatalf("expected no tags, got %v", tags)
	}

	// Create events with tags
	http.Post(ts.URL+"/api/events", "application/json",
		bytes.NewBufferString(`{"timestamp":"2026-04-01T14:00:00+09:00","tags":["beta","alpha"]}`))
	http.Post(ts.URL+"/api/events", "application/json",
		bytes.NewBufferString(`{"timestamp":"2026-04-01T15:00:00+09:00","tags":["alpha","gamma"]}`))

	resp, _ = http.Get(ts.URL + "/api/tags")
	json.NewDecoder(resp.Body).Decode(&tags)
	resp.Body.Close()
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %v", tags)
	}
}

func TestExportMarkdownEndpoint(t *testing.T) {
	_, ts := testServer(t)

	// Set up data
	req, _ := http.NewRequest("PUT", ts.URL+"/api/meta",
		bytes.NewBufferString(`{"title":"Export Test","case_id":"EXP-001"}`))
	req.Header.Set("Content-Type", "application/json")
	http.DefaultClient.Do(req)

	http.Post(ts.URL+"/api/events", "application/json",
		bytes.NewBufferString(`{"timestamp":"2026-04-01T14:00:00+09:00","description":"Event 1","tags":["detection"]}`))

	resp, _ := http.Get(ts.URL + "/api/export/markdown")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") != "text/markdown; charset=utf-8" {
		t.Fatalf("unexpected content-type: %s", resp.Header.Get("Content-Type"))
	}
	md := string(body)
	if !contains(md, "EXP-001") || !contains(md, "Export Test") || !contains(md, "Event 1") {
		t.Fatalf("markdown export missing expected content:\n%s", md)
	}
}

func TestGetTimezoneDefault(t *testing.T) {
	_, ts := testServer(t)
	resp, err := http.Get(ts.URL + "/api/timezone")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var m map[string]string
	json.NewDecoder(resp.Body).Decode(&m)
	// Default timezone is empty string since testServer doesn't pass one
	if _, ok := m["timezone"]; !ok {
		t.Fatal("expected timezone key in response")
	}
}

func TestGetTimezoneFromMeta(t *testing.T) {
	store, ts := testServer(t)
	// Set timezone in meta
	if err := store.SetMeta("timezone", "Asia/Tokyo"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(ts.URL + "/api/timezone")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]string
	json.NewDecoder(resp.Body).Decode(&m)
	if m["timezone"] != "Asia/Tokyo" {
		t.Fatalf("expected Asia/Tokyo, got %q", m["timezone"])
	}
}

func TestTimezoneViaMetaPut(t *testing.T) {
	_, ts := testServer(t)
	// Set timezone via PUT /api/meta
	body := `{"timezone":"America/New_York"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/meta", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Read back via GET /api/timezone
	resp, err = http.Get(ts.URL + "/api/timezone")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]string
	json.NewDecoder(resp.Body).Decode(&m)
	if m["timezone"] != "America/New_York" {
		t.Fatalf("expected America/New_York, got %q", m["timezone"])
	}
}

func TestServeIndex(t *testing.T) {
	_, ts := testServer(t)
	resp, _ := http.Get(ts.URL + "/")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if string(body) != "<html></html>" {
		t.Fatalf("unexpected body: %s", body)
	}
}
