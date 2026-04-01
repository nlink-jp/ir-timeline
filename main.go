package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

//go:embed web/*
var webFS embed.FS

var version = "dev"

func main() {
	// Peek at first non-flag arg for subcommand
	if len(os.Args) > 1 && os.Args[1] == "import" {
		runImport(os.Args[2:])
		return
	}
	runServe()
}

func runServe() {
	dbPath := flag.String("db", "timeline.db", "SQLite database path")
	port := flag.Int("port", 8888, "HTTP server port")
	noBrowser := flag.Bool("no-browser", false, "don't auto-open browser")
	tz := flag.String("tz", "", "IANA timezone (e.g. Asia/Tokyo); defaults to system local")
	showVersion := flag.Bool("version", false, "show version")
	flag.Parse()

	if *showVersion {
		fmt.Println("ir-timeline", version)
		return
	}

	store, err := NewStorage(*dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	timezone := resolveTimezone(*tz, store)

	h := NewHandler(store, webFS, timezone)

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	url := fmt.Sprintf("http://%s", addr)

	if !*noBrowser {
		go openBrowser(url)
	}

	srv := &http.Server{Addr: addr, Handler: h}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("ir-timeline %s running at %s (db: %s, tz: %s)", version, url, *dbPath, timezone)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-quit
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}
	log.Println("stopped")
}

func runImport(args []string) {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	dbPath := fs.String("db", "timeline.db", "SQLite database path")
	format := fs.String("format", "", "input format: json or csv (auto-detected from extension if omitted)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: ir-timeline import [flags] <file>\n\n")
		fmt.Fprintf(os.Stderr, "Import events from a JSON or CSV file.\n\n")
		fmt.Fprintf(os.Stderr, "JSON format:\n")
		fmt.Fprintf(os.Stderr, "  [{\"timestamp\":\"...\",\"description\":\"...\",\"actor\":\"...\",\"tags\":[\"...\"]}, ...]\n\n")
		fmt.Fprintf(os.Stderr, "CSV format (header required):\n")
		fmt.Fprintf(os.Stderr, "  timestamp,timestamp_end,description,actor,tags\n")
		fmt.Fprintf(os.Stderr, "  2026-04-01T14:00:00+09:00,,Phishing reported,SOC,\"detection,analysis\"\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}
	// Reorder args: move non-flag args to the end so flags after the file path work
	// e.g. "import file.json --db x.db" → "--db x.db file.json"
	reordered := reorderArgs(args)
	fs.Parse(reordered)

	if fs.NArg() < 1 {
		fs.Usage()
		os.Exit(1)
	}
	filePath := fs.Arg(0)

	store, err := NewStorage(*dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	n, err := importFile(store, filePath, *format)
	if err != nil {
		log.Fatalf("import failed: %v", err)
	}
	fmt.Printf("imported %d events into %s\n", n, *dbPath)
}

// resolveTimezone determines the timezone from CLI flag, DB meta, or system local.
func resolveTimezone(flagTZ string, store *Storage) string {
	if flagTZ != "" {
		if _, err := time.LoadLocation(flagTZ); err != nil {
			log.Fatalf("invalid timezone %q: %v", flagTZ, err)
		}
		_ = store.SetMeta("timezone", flagTZ)
		return flagTZ
	}
	if dbTZ, err := store.GetMeta("timezone"); err == nil && dbTZ != "" {
		if _, err := time.LoadLocation(dbTZ); err == nil {
			return dbTZ
		}
	}
	name := time.Now().Location().String()
	// time.Local.String() returns "Local" which is not a valid IANA name.
	// Read the TZ env var or fall back to UTC.
	if name == "Local" {
		if tz := os.Getenv("TZ"); tz != "" {
			name = tz
		} else {
			// Try to read system timezone on macOS/Linux
			if link, err := os.Readlink("/etc/localtime"); err == nil {
				// /etc/localtime -> /var/db/timezone/zoneinfo/Asia/Tokyo
				if idx := strings.Index(link, "zoneinfo/"); idx >= 0 {
					name = link[idx+len("zoneinfo/"):]
				}
			}
			if name == "Local" {
				name = "UTC"
			}
		}
	}
	_ = store.SetMeta("timezone", name)
	return name
}

// reorderArgs moves flags (--key val) before positional args so flag.Parse works
// regardless of argument order. e.g. ["file.json", "--db", "x.db"] → ["--db", "x.db", "file.json"]
func reorderArgs(args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		if len(args[i]) > 0 && args[i][0] == '-' {
			flags = append(flags, args[i])
			// If this flag has a value (next arg doesn't start with -), consume it too
			if i+1 < len(args) && (len(args[i+1]) == 0 || args[i+1][0] != '-') {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			positional = append(positional, args[i])
		}
	}
	return append(flags, positional...)
}

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		return
	}
	exec.Command(cmd, args...).Start()
}
