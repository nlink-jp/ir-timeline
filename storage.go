package main

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Storage wraps SQLite operations for a single incident timeline.
type Storage struct {
	db *sql.DB
}

// Event represents a timeline event.
// TimestampEnd is optional — if set, the event spans a time range.
// Timestamps are stored in UTC (ISO 8601). InputTZ records the original timezone.
type Event struct {
	ID           int64    `json:"id"`
	Timestamp    string   `json:"timestamp"`
	TimestampEnd *string  `json:"timestamp_end"`
	InputTZ      string   `json:"input_tz"`
	Description  string   `json:"description"`
	Actor        string   `json:"actor"`
	Tags         []string `json:"tags"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
	Images       []Image  `json:"images"`
}

// Image represents an attached image (without binary data).
type Image struct {
	ID          int64  `json:"id"`
	EventID     int64  `json:"event_id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	CreatedAt   string `json:"created_at"`
}

// ImageData includes the binary content for serving.
type ImageData struct {
	Image
	Data []byte `json:"-"`
}

// NewStorage opens or creates a SQLite database at path.
func NewStorage(path string) (*Storage, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable FK: %w", err)
	}
	s := &Storage{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

func (s *Storage) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS events (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp     TEXT NOT NULL,
			timestamp_end TEXT,
			input_tz      TEXT NOT NULL DEFAULT 'UTC',
			description   TEXT NOT NULL DEFAULT '',
			actor         TEXT NOT NULL DEFAULT '',
			created_at    TEXT NOT NULL DEFAULT (datetime('now', 'localtime')),
			updated_at    TEXT NOT NULL DEFAULT (datetime('now', 'localtime'))
		);
		CREATE TABLE IF NOT EXISTS event_tags (
			event_id INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
			tag      TEXT NOT NULL,
			PRIMARY KEY (event_id, tag)
		);
		CREATE TABLE IF NOT EXISTS event_images (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id     INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
			filename     TEXT NOT NULL,
			content_type TEXT NOT NULL,
			data         BLOB NOT NULL,
			created_at   TEXT NOT NULL DEFAULT (datetime('now', 'localtime'))
		);
		CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
		CREATE INDEX IF NOT EXISTS idx_event_tags_tag ON event_tags(tag);
		CREATE INDEX IF NOT EXISTS idx_event_images_event_id ON event_images(event_id);
	`)
	if err != nil {
		return err
	}

	// Incremental migrations for existing DBs
	s.addColumnIfNotExists("events", "timestamp_end", "TEXT")
	s.addColumnIfNotExists("events", "input_tz", "TEXT NOT NULL DEFAULT 'UTC'")

	return nil
}

func (s *Storage) addColumnIfNotExists(table, column, colType string) {
	// PRAGMA table_info returns rows with: cid, name, type, notnull, dflt_value, pk
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return
		}
		if name == column {
			return // already exists
		}
	}
	s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + colType)
}

// --- Meta ---

func (s *Storage) GetMeta(key string) (string, error) {
	var val string
	err := s.db.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

func (s *Storage) SetMeta(key, value string) error {
	_, err := s.db.Exec(
		"INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	return err
}

func (s *Storage) GetAllMeta() (map[string]string, error) {
	rows, err := s.db.Query("SELECT key, value FROM meta")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

// --- Events ---

func (s *Storage) ListEvents() ([]Event, error) {
	rows, err := s.db.Query("SELECT id, timestamp, timestamp_end, input_tz, description, actor, created_at, updated_at FROM events ORDER BY timestamp ASC, id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.TimestampEnd, &e.InputTZ, &e.Description, &e.Actor, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load tags and images for each event
	for i := range events {
		tags, err := s.getEventTags(events[i].ID)
		if err != nil {
			return nil, err
		}
		events[i].Tags = tags

		images, err := s.getEventImages(events[i].ID)
		if err != nil {
			return nil, err
		}
		events[i].Images = images
	}

	// Sort by parsed timestamp (handles mixed TZ formats correctly)
	sort.SliceStable(events, func(i, j int) bool {
		ti := parseTimestamp(events[i].Timestamp)
		tj := parseTimestamp(events[j].Timestamp)
		if ti.Equal(tj) {
			return events[i].ID < events[j].ID
		}
		return ti.Before(tj)
	})

	return events, nil
}

// formatTimestampInTZ converts a UTC timestamp to the given location for display.
// Returns "2006-01-02 15:04:05" format. Falls back to raw string if unparseable.
func formatTimestampInTZ(ts string, loc *time.Location) string {
	t := parseTimestamp(ts)
	if t.IsZero() {
		return ts
	}
	if loc != nil {
		t = t.In(loc)
	}
	return t.Format("2006-01-02 15:04:05")
}

// toUTC parses a timestamp string and returns it as UTC ISO 8601.
// If already UTC or unparseable, returns as-is.
func toUTC(s string) string {
	t := parseTimestamp(s)
	if t.IsZero() {
		return s
	}
	return t.UTC().Format(time.RFC3339)
}

// toUTCPtr is toUTC for optional timestamps.
func toUTCPtr(s *string) *string {
	if s == nil || *s == "" {
		return s
	}
	v := toUTC(*s)
	return &v
}

// parseTimestamp tries multiple formats and returns the parsed time.
// Handles RFC3339, RFC3339 with millis, and common datetime formats.
func parseTimestamp(s string) time.Time {
	layouts := []string{
		time.RFC3339Nano,          // 2006-01-02T15:04:05.999999999Z07:00
		time.RFC3339,              // 2006-01-02T15:04:05Z07:00
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (s *Storage) GetEvent(id int64) (*Event, error) {
	var e Event
	err := s.db.QueryRow(
		"SELECT id, timestamp, timestamp_end, input_tz, description, actor, created_at, updated_at FROM events WHERE id = ?", id,
	).Scan(&e.ID, &e.Timestamp, &e.TimestampEnd, &e.InputTZ, &e.Description, &e.Actor, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	tags, err := s.getEventTags(e.ID)
	if err != nil {
		return nil, err
	}
	e.Tags = tags
	images, err := s.getEventImages(e.ID)
	if err != nil {
		return nil, err
	}
	e.Images = images
	return &e, nil
}

func (s *Storage) CreateEvent(timestamp string, timestampEnd *string, description, actor, inputTZ string, tags []string) (*Event, error) {
	now := time.Now().Format("2006-01-02 15:04:05")
	if inputTZ == "" {
		inputTZ = "UTC"
	}
	res, err := s.db.Exec(
		"INSERT INTO events (timestamp, timestamp_end, input_tz, description, actor, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		toUTC(timestamp), toUTCPtr(timestampEnd), inputTZ, description, actor, now, now,
	)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := s.setEventTags(id, tags); err != nil {
		return nil, err
	}
	return s.GetEvent(id)
}

func (s *Storage) UpdateEvent(id int64, timestamp string, timestampEnd *string, description, actor, inputTZ string, tags []string) (*Event, error) {
	now := time.Now().Format("2006-01-02 15:04:05")
	if inputTZ == "" {
		inputTZ = "UTC"
	}
	res, err := s.db.Exec(
		"UPDATE events SET timestamp = ?, timestamp_end = ?, input_tz = ?, description = ?, actor = ?, updated_at = ? WHERE id = ?",
		toUTC(timestamp), toUTCPtr(timestampEnd), inputTZ, description, actor, now, id,
	)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, sql.ErrNoRows
	}
	if err := s.setEventTags(id, tags); err != nil {
		return nil, err
	}
	return s.GetEvent(id)
}

func (s *Storage) DeleteEvent(id int64) error {
	res, err := s.db.Exec("DELETE FROM events WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- Tags ---

func (s *Storage) getEventTags(eventID int64) ([]string, error) {
	rows, err := s.db.Query("SELECT tag FROM event_tags WHERE event_id = ? ORDER BY tag", eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, rows.Err()
}

func (s *Storage) setEventTags(eventID int64, tags []string) error {
	if _, err := s.db.Exec("DELETE FROM event_tags WHERE event_id = ?", eventID); err != nil {
		return err
	}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, err := s.db.Exec("INSERT INTO event_tags (event_id, tag) VALUES (?, ?)", eventID, tag); err != nil {
			return err
		}
	}
	return nil
}

func (s *Storage) ListTags() ([]string, error) {
	rows, err := s.db.Query("SELECT DISTINCT tag FROM event_tags ORDER BY tag")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, rows.Err()
}

// --- Images ---

func (s *Storage) getEventImages(eventID int64) ([]Image, error) {
	rows, err := s.db.Query(
		"SELECT id, event_id, filename, content_type, created_at FROM event_images WHERE event_id = ? ORDER BY id",
		eventID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var images []Image
	for rows.Next() {
		var img Image
		if err := rows.Scan(&img.ID, &img.EventID, &img.Filename, &img.ContentType, &img.CreatedAt); err != nil {
			return nil, err
		}
		images = append(images, img)
	}
	if images == nil {
		images = []Image{}
	}
	return images, rows.Err()
}

func (s *Storage) CreateImage(eventID int64, filename, contentType string, data []byte) (*Image, error) {
	now := time.Now().Format("2006-01-02 15:04:05")
	res, err := s.db.Exec(
		"INSERT INTO event_images (event_id, filename, content_type, data, created_at) VALUES (?, ?, ?, ?, ?)",
		eventID, filename, contentType, data, now,
	)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Image{ID: id, EventID: eventID, Filename: filename, ContentType: contentType, CreatedAt: now}, nil
}

func (s *Storage) GetImage(id int64) (*ImageData, error) {
	var img ImageData
	err := s.db.QueryRow(
		"SELECT id, event_id, filename, content_type, data, created_at FROM event_images WHERE id = ?", id,
	).Scan(&img.ID, &img.EventID, &img.Filename, &img.ContentType, &img.Data, &img.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &img, nil
}

func (s *Storage) DeleteImage(id int64) error {
	res, err := s.db.Exec("DELETE FROM event_images WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- Export ---

func (s *Storage) ExportMarkdown() (string, error) {
	meta, err := s.GetAllMeta()
	if err != nil {
		return "", err
	}
	events, err := s.ListEvents()
	if err != nil {
		return "", err
	}

	// Load timezone for display
	tz := meta["timezone"]
	var loc *time.Location
	if tz != "" {
		loc, _ = time.LoadLocation(tz)
	}

	var b strings.Builder
	b.WriteString("# ")
	if caseID := meta["case_id"]; caseID != "" {
		b.WriteString("[" + caseID + "] ")
	}
	if title := meta["title"]; title != "" {
		b.WriteString(title)
	} else {
		b.WriteString("Incident Timeline")
	}
	b.WriteString("\n\n")
	if tz != "" {
		b.WriteString(fmt.Sprintf("**Timezone:** %s\n\n", tz))
	}

	for i, e := range events {
		timeStr := formatTimestampInTZ(e.Timestamp, loc)
		if e.TimestampEnd != nil && *e.TimestampEnd != "" {
			timeStr += " — " + formatTimestampInTZ(*e.TimestampEnd, loc)
		}
		b.WriteString(fmt.Sprintf("## %s", timeStr))
		if len(e.Tags) > 0 {
			b.WriteString("  [" + strings.Join(e.Tags, ", ") + "]")
		}
		b.WriteString("\n\n")
		if e.Actor != "" {
			b.WriteString(fmt.Sprintf("**Actor:** %s\n\n", e.Actor))
		}
		if e.Description != "" {
			b.WriteString(e.Description + "\n\n")
		}
		if len(e.Images) > 0 {
			for _, img := range e.Images {
				b.WriteString(fmt.Sprintf("- [Image: %s]\n", img.Filename))
			}
			b.WriteString("\n")
		}
		// Time delta to next event
		if i < len(events)-1 {
			delta := calcTimeDelta(e.Timestamp, events[i+1].Timestamp)
			if delta != "" {
				b.WriteString(fmt.Sprintf("*+%s*\n\n", delta))
			}
		}
		b.WriteString("---\n\n")
	}
	return b.String(), nil
}

func calcTimeDelta(from, to string) string {
	t1 := parseTimestamp(from)
	t2 := parseTimestamp(to)
	if t1.IsZero() || t2.IsZero() {
		return ""
	}
	d := t2.Sub(t1)
	if d < 0 {
		return ""
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}
