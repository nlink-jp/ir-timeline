package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
)

const maxImageSize = 10 << 20 // 10MB

// NewHandler creates the HTTP handler with all routes.
func NewHandler(store *Storage, webFS fs.FS, timezone ...string) http.Handler {
	tz := ""
	if len(timezone) > 0 {
		tz = timezone[0]
	}

	mux := http.NewServeMux()

	// Static files (SPA)
	webSub, _ := fs.Sub(webFS, "web")
	fileServer := http.FileServer(http.FS(webSub))
	mux.Handle("GET /", fileServer)

	// Meta
	mux.HandleFunc("GET /api/meta", handleGetMeta(store))
	mux.HandleFunc("PUT /api/meta", handlePutMeta(store))

	// Timezone
	mux.HandleFunc("GET /api/timezone", handleGetTimezone(store, tz))

	// Events
	mux.HandleFunc("GET /api/events", handleListEvents(store))
	mux.HandleFunc("POST /api/events", handleCreateEvent(store))
	mux.HandleFunc("PUT /api/events/{id}", handleUpdateEvent(store))
	mux.HandleFunc("DELETE /api/events/{id}", handleDeleteEvent(store))

	// Images
	mux.HandleFunc("POST /api/events/{id}/images", handleUploadImage(store))
	mux.HandleFunc("GET /api/images/{id}", handleGetImage(store))
	mux.HandleFunc("DELETE /api/images/{id}", handleDeleteImage(store))

	// Tags
	mux.HandleFunc("GET /api/tags", handleListTags(store))

	// Export
	mux.HandleFunc("GET /api/export/markdown", handleExportMarkdown(store))

	return mux
}

// --- helpers ---

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

// --- Meta ---

func handleGetMeta(store *Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m, err := store.GetAllMeta()
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, m)
	}
}

func handlePutMeta(store *Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		for k, v := range body {
			if err := store.SetMeta(k, v); err != nil {
				jsonError(w, 500, err.Error())
				return
			}
		}
		jsonOK(w, map[string]bool{"ok": true})
	}
}

// --- Timezone ---

func handleGetTimezone(store *Storage, defaultTZ string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tz, err := store.GetMeta("timezone")
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		if tz == "" {
			tz = defaultTZ
		}
		jsonOK(w, map[string]string{"timezone": tz})
	}
}

// --- Events ---

type eventRequest struct {
	Timestamp    string   `json:"timestamp"`
	TimestampEnd *string  `json:"timestamp_end"`
	InputTZ      string   `json:"input_tz"`
	Description  string   `json:"description"`
	Actor        string   `json:"actor"`
	Tags         []string `json:"tags"`
}

func handleListEvents(store *Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events, err := store.ListEvents()
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, events)
	}
}

func handleCreateEvent(store *Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req eventRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		if req.Timestamp == "" {
			jsonError(w, 400, "timestamp is required")
			return
		}
		event, err := store.CreateEvent(req.Timestamp, req.TimestampEnd, req.Description, req.Actor, req.InputTZ, req.Tags)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		w.WriteHeader(201)
		jsonOK(w, event)
	}
}

func handleUpdateEvent(store *Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			jsonError(w, 400, "invalid id")
			return
		}
		var req eventRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		if req.Timestamp == "" {
			jsonError(w, 400, "timestamp is required")
			return
		}
		event, err := store.UpdateEvent(id, req.Timestamp, req.TimestampEnd, req.Description, req.Actor, req.InputTZ, req.Tags)
		if err == sql.ErrNoRows {
			jsonError(w, 404, "event not found")
			return
		}
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, event)
	}
}

func handleDeleteEvent(store *Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			jsonError(w, 400, "invalid id")
			return
		}
		if err := store.DeleteEvent(id); err == sql.ErrNoRows {
			jsonError(w, 404, "event not found")
			return
		} else if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, map[string]bool{"ok": true})
	}
}

// --- Images ---

func handleUploadImage(store *Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		eventID, err := pathID(r)
		if err != nil {
			jsonError(w, 400, "invalid id")
			return
		}
		// Check event exists
		if _, err := store.GetEvent(eventID); err == sql.ErrNoRows {
			jsonError(w, 404, "event not found")
			return
		} else if err != nil {
			jsonError(w, 500, err.Error())
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxImageSize+1024)
		if err := r.ParseMultipartForm(maxImageSize); err != nil {
			jsonError(w, 400, "file too large (max 10MB)")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			jsonError(w, 400, "missing file field")
			return
		}
		defer file.Close()

		data, err := io.ReadAll(file)
		if err != nil {
			jsonError(w, 400, "failed to read file")
			return
		}

		// Determine content type: prefer multipart header, fall back to sniffing
		ct := header.Header.Get("Content-Type")
		if ct == "" || ct == "application/octet-stream" {
			ct = http.DetectContentType(data)
		}
		if !strings.HasPrefix(ct, "image/") {
			jsonError(w, 400, fmt.Sprintf("invalid content type: %s (must be image/*)", ct))
			return
		}

		img, err := store.CreateImage(eventID, header.Filename, ct, data)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		w.WriteHeader(201)
		jsonOK(w, img)
	}
}

func handleGetImage(store *Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			jsonError(w, 400, "invalid id")
			return
		}
		img, err := store.GetImage(id)
		if err == sql.ErrNoRows {
			jsonError(w, 404, "image not found")
			return
		}
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		w.Header().Set("Content-Type", img.ContentType)
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.Write(img.Data)
	}
}

func handleDeleteImage(store *Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			jsonError(w, 400, "invalid id")
			return
		}
		if err := store.DeleteImage(id); err == sql.ErrNoRows {
			jsonError(w, 404, "image not found")
			return
		} else if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, map[string]bool{"ok": true})
	}
}

// --- Tags ---

func handleListTags(store *Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tags, err := store.ListTags()
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, tags)
	}
}

// --- Export ---

func handleExportMarkdown(store *Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		md, err := store.ExportMarkdown()
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=timeline.md")
		w.Write([]byte(md))
	}
}
