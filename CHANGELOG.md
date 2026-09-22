# Changelog

## v0.2.0 (2026-07-12)

### Removed

- **darwin/amd64 (Intel) pre-built binary.** macOS releases now ship
  **arm64 only**, per the org-wide policy (darwin is Apple-Silicon only; no
  universal binaries). Intel Mac users can build from source.

### Changed

- **Linux release archives are now `.tar.gz`** (darwin/windows remain `.zip`),
  per `nlink-jp/.github` CONVENTIONS.md §Release Archive Standard. Archives
  already bundled `LICENSE` + `README.md` alongside the canonical binary.
- **darwin code-signature identifier** is now the canonical `ir-timeline`
  (was `ir-timeline-darwin-arm64`), set via `codesign -i` so it stays stable
  after the archived binary is renamed to its canonical name.

No change to the binary's behaviour — a packaging / build-config release.

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
## [Unreleased]

### Fixed

- **`make verify-release` now fails closed.** Its last block chained unzip, the
  packaged binary's `--version` and `spctl` with `&&` and ended the whole chain
  in `|| true`, so a zip that did not unpack or a binary that did not run exited
  0 and the upload proceeded. Each step is now judged on its own, the packaged
  binary's `--version` must contain the tag being released, and only the
  informational `spctl` line may be ignored. Matches the org template
  (CONVENTIONS.md §Code Signing → Verifying a release).
- **The Linux archives no longer carry macOS file metadata.** macOS `tar` wrote
  each bundled file's extended attributes (`com.apple.provenance`, and a Dropbox
  attribute where the tree is synced) into the `.tar.gz` twice: as AppleDouble
  `._` members, which GNU tar extracts as stray `._<name>` files beside the real
  ones, and as `LIBARCHIVE.xattr.*` / `SCHILY.xattr.*` pax headers, which it
  reports as unknown keywords. `make package` now archives with
  `COPYFILE_DISABLE=1 tar --no-xattrs`; each setting stops one of the two.
  Archives already published still carry them; the files themselves are
  unaffected.

### Internal

- `make verify-release` also judges each Linux archive: no AppleDouble or other
  macOS metadata members — listed with `--options 'tar:!mac-ext'`, because a
  plain macOS listing folds `._` members away — no extended attributes as pax
  headers, and exactly the canonical binary, `README.md` and `LICENSE`, compared
  in the C locale.

