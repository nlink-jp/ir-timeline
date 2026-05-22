# Changelog

## v0.1.1 (2026-05-23)

### Added

- **`package` Makefile target.** Builds all 5 platforms, signs darwin
  binaries with Developer ID, zips each with LICENSE + README.md
  using versioned naming
  (`ir-timeline-vX.Y.Z-<os>-<arch>.zip`), and notarizes the
  darwin zips. Replaces the manual zip step that produced the
  v0.1.0 release.

### Changed

- **Darwin releases are now Developer ID signed and Apple-notarized.**
  `ir-timeline-v0.1.1-darwin-{amd64,arm64}.zip` carry full Apple
  Developer ID Application signatures and notarization tickets from
  Apple. End users on macOS no longer need to bypass Gatekeeper
  with right-click → Open or `xattr -d com.apple.quarantine` on
  first launch; local users who place `ir-timeline` under
  Dropbox-synced (or any other FileProvider-managed) paths are no
  longer killed by macOS's ad-hoc + provenance distrust policy.
  Pipeline: `scripts/codesign-darwin.sh` +
  `scripts/notarize-darwin.sh`, driven by `make package`. Adopts
  the org-wide convention in `nlink-jp/.github` CONVENTIONS.md
  §Code Signing.

No behaviour change to the binary itself — feature-wise this is
identical to v0.1.0.

## v0.1.0 (2026-04-02)

### Added
- Initial release
- Web UI with List View (vertical timeline) and Chart View (horizontal swimlane)
- Event CRUD with timestamp, description, actor, multiple tags
- Image attachment (drag & drop, BLOB storage in SQLite)
- Tag-based color coding and filtering
- Time delta display between events
- Dark / Light theme toggle
- Incident metadata (title, case ID)
- Markdown export
- Auto-open browser on launch
