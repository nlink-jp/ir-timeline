package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type importEvent struct {
	Timestamp    string   `json:"timestamp"`
	TimestampEnd *string  `json:"timestamp_end"`
	Description  string   `json:"description"`
	Actor        string   `json:"actor"`
	Tags         []string `json:"tags"`
}

// importFile reads events from a JSON or CSV file and inserts them into the DB.
// Returns the number of events imported.
func importFile(store *Storage, path, format string) (int, error) {
	if format == "" {
		format = detectFormat(path)
	}

	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	switch format {
	case "json":
		return importJSON(store, f)
	case "csv":
		return importCSV(store, f)
	default:
		return 0, fmt.Errorf("unknown format %q (use --format json or --format csv)", format)
	}
}

func detectFormat(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return "json"
	case ".csv":
		return "csv"
	default:
		return "json"
	}
}

func importJSON(store *Storage, r io.Reader) (int, error) {
	var events []importEvent
	if err := json.NewDecoder(r).Decode(&events); err != nil {
		return 0, fmt.Errorf("parse JSON: %w", err)
	}
	for i, e := range events {
		if e.Timestamp == "" {
			return i, fmt.Errorf("event %d: missing timestamp", i+1)
		}
		_, err := store.CreateEvent(e.Timestamp, e.TimestampEnd, e.Description, e.Actor, "", e.Tags)
		if err != nil {
			return i, fmt.Errorf("event %d: %w", i+1, err)
		}
	}
	return len(events), nil
}

func importCSV(store *Storage, r io.Reader) (int, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true

	// Read header
	header, err := reader.Read()
	if err != nil {
		return 0, fmt.Errorf("read CSV header: %w", err)
	}
	colMap := make(map[string]int)
	for i, h := range header {
		colMap[strings.TrimSpace(strings.ToLower(h))] = i
	}
	if _, ok := colMap["timestamp"]; !ok {
		return 0, fmt.Errorf("CSV header must contain 'timestamp' column")
	}

	count := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, fmt.Errorf("row %d: %w", count+2, err)
		}

		get := func(col string) string {
			if idx, ok := colMap[col]; ok && idx < len(record) {
				return strings.TrimSpace(record[idx])
			}
			return ""
		}

		timestamp := get("timestamp")
		if timestamp == "" {
			return count, fmt.Errorf("row %d: missing timestamp", count+2)
		}

		var timestampEnd *string
		if v := get("timestamp_end"); v != "" {
			timestampEnd = &v
		}

		description := get("description")
		actor := get("actor")

		var tags []string
		if v := get("tags"); v != "" {
			for _, t := range strings.Split(v, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					tags = append(tags, t)
				}
			}
		}

		inputTZ := get("input_tz")
		_, err = store.CreateEvent(timestamp, timestampEnd, description, actor, inputTZ, tags)
		if err != nil {
			return count, fmt.Errorf("row %d: %w", count+2, err)
		}
		count++
	}
	return count, nil
}
