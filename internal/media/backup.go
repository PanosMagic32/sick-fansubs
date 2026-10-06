package media

import (
	"archive/tar"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Extraction bounds bound a tampered tar's disk usage, not production media
// sizing; the per-file cap is 8× MaxInputBytes (10 MiB). The detail is in
// internal/media/AGENTS.md.
const (
	maxExtractFileBytes  = 80 << 20  // 80 MiB per extracted file
	maxExtractTotalBytes = 100 << 30 // 100 GiB total across one extraction
)

// maxManifestBytes bounds a manifest read (exactly one JSON value, small).
const maxManifestBytes = 1 << 20

// MediaManifest is the backup sidecar that binds the three unit artifacts.
type MediaManifest struct {
	// BackupTimestampMS is the unit's creation time in UTC milliseconds.
	BackupTimestampMS int64 `json:"backupTimestampMs"`
	// DBFileName is the database artifact's clean single-component name.
	DBFileName string `json:"dbFileName"`
	// MediaTarFileName is the tarball's clean single-component name.
	MediaTarFileName string `json:"mediaTarFileName"`
	// MediaTarSHA256 is the tarball's lowercase-hex SHA-256.
	MediaTarSHA256 string `json:"mediaTarSha256"`
}

// ManifestPathFor derives the manifest path from the database backup path —
// "a JSON file at the same path with a .media-manifest.json suffix".
// This is the single source of truth for discovery: cmd/backup
// writes here, cmd/restore reads here.
func ManifestPathFor(dbBackupPath string) string {
	return dbBackupPath + ".media-manifest.json"
}

// WriteMediaManifest writes the manifest as 0600 via a temp file + rename so
// a crash can never leave a truncated manifest masquerading as a valid unit.
func WriteMediaManifest(path string, m MediaManifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("media: encode manifest: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("media: write manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		// Best-effort removal of the temp file; the rename already failed.
		_ = os.Remove(tmp)
		return fmt.Errorf("media: publish manifest: %w", err)
	}
	return nil
}

// ReadMediaManifest decodes the manifest strictly: bounded size (an
// over-bound file is refused by the explicit length check, not silently
// truncated), exactly one JSON value, no unknown fields.
func ReadMediaManifest(path string) (MediaManifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return MediaManifest{}, fmt.Errorf("media: open manifest: %w", err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxManifestBytes+1))
	if err != nil {
		return MediaManifest{}, fmt.Errorf("media: read manifest: %w", err)
	}
	if len(data) > maxManifestBytes {
		return MediaManifest{}, fmt.Errorf("media: manifest exceeds the %d-byte size bound", maxManifestBytes)
	}

	var m MediaManifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return MediaManifest{}, fmt.Errorf("media: decode manifest: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return MediaManifest{}, errors.New("media: manifest contains more than one JSON value")
	}
	return m, nil
}

// BuildMediaTar streams <dataDir>/media into destDir/tarName as a
// deterministic tar (sorted walk, zeroed timestamps, regular files only) and
// returns its SHA-256 hex; the publication contract is in internal/media/AGENTS.md.
func BuildMediaTar(dataDir, destDir, tarName string) (string, error) {
	mediaRoot := filepath.Join(dataDir, "media")
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return "", fmt.Errorf("media: create backup directory: %w", err)
	}

	tmp := filepath.Join(destDir, ".tmp-"+randomSuffix()+".tar")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("media: create tarball: %w", err)
	}
	cleanup := func() {
		// Best-effort cleanup on an already-failing path.
		f.Close()
		_ = os.Remove(tmp)
	}

	hasher := sha256.New()
	tw := tar.NewWriter(io.MultiWriter(f, hasher))

	if _, err := os.Stat(mediaRoot); err == nil {
		walkErr := filepath.WalkDir(mediaRoot, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				return fmt.Errorf("media: tarball source %q is not a regular file", d.Name())
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(mediaRoot, p)
			if err != nil {
				return err
			}
			hdr := &tar.Header{
				Name:    filepath.ToSlash(rel),
				Mode:    0o644,
				Size:    info.Size(),
				ModTime: time.Unix(0, 0).UTC(),
				Format:  tar.FormatUSTAR,
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return fmt.Errorf("media: tarball header %q: %w", rel, err)
			}
			src, err := os.Open(p)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(tw, src)
			closeErr := src.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			return nil
		})
		if walkErr != nil {
			cleanup()
			return "", fmt.Errorf("media: build tarball: %w", walkErr)
		}
	} else if !os.IsNotExist(err) {
		cleanup()
		return "", fmt.Errorf("media: stat media directory: %w", err)
	}

	if err := tw.Close(); err != nil {
		cleanup()
		return "", fmt.Errorf("media: close tarball: %w", err)
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return "", fmt.Errorf("media: sync tarball: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", fmt.Errorf("media: close tarball: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(destDir, tarName)); err != nil {
		cleanup()
		return "", fmt.Errorf("media: publish tarball: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// ValidateManifest checks the sidecar's field contract before restore work
// starts (cmd/restore's pre-activation gate): every field populated, both
// filenames clean single-component relative names (no separators, no "." or
// ".."), the SHA-256 exactly 64 lowercase hex characters, and a positive
// backup timestamp. The sidecar is integrity metadata, not trusted input — a
// corrupt or malicious manifest must never steer extraction outside its own
// unit directory.
func ValidateManifest(m MediaManifest) error {
	if m.BackupTimestampMS <= 0 {
		return errors.New("media: manifest carries a non-positive backup timestamp")
	}
	if !safeManifestFileName(m.DBFileName) {
		return fmt.Errorf("media: manifest database filename %q is not a plain file name", m.DBFileName)
	}
	if !safeManifestFileName(m.MediaTarFileName) {
		return fmt.Errorf("media: manifest tarball filename %q is not a plain file name", m.MediaTarFileName)
	}
	if len(m.MediaTarSHA256) != 64 {
		return errors.New("media: manifest SHA-256 must be exactly 64 hex characters")
	}
	for _, r := range m.MediaTarSHA256 {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return errors.New("media: manifest SHA-256 must be lowercase hex")
		}
	}
	return nil
}

// safeManifestFileName reports whether name is a clean single-component
// relative file name: non-empty, no path separators (forward or backslash),
// and not a "."/".." component. VerifyMediaTar reuses it so a hostile sidecar
// cannot address files outside its own unit directory.
func safeManifestFileName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, `/\`)
}

// VerifyMediaTar checks the manifest's tarball exists and its SHA-256
// matches the manifest. This runs BEFORE any restore activation (a manifest
// whose verification fails is rejected before activation). The
// comparison is plain equality — the manifest is integrity metadata, not an
// authentication boundary (in production both files are age ciphertext, so
// tampering already implies the unit is compromised).
func VerifyMediaTar(unitDir string, m MediaManifest) error {
	if !safeManifestFileName(m.MediaTarFileName) {
		return fmt.Errorf("media: manifest tarball filename %q is not a plain file name", m.MediaTarFileName)
	}
	f, err := os.Open(TarPathFor(unitDir, m))
	if err != nil {
		return fmt.Errorf("media: open tarball: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("media: hash tarball: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != m.MediaTarSHA256 {
		return fmt.Errorf("media: tarball checksum mismatch: got %s, manifest says %s", got, m.MediaTarSHA256)
	}
	return nil
}

// TarPathFor returns the unit's tarball path: the manifest's tarball
// filename resolved inside unitDir. Callers must run ValidateManifest first
// — this does not re-check the name. It is the single source for the join so
// verification (VerifyMediaTar) and extraction (cmd/restore →
// ExtractMediaTar) can never disagree on the path.
func TarPathFor(unitDir string, m MediaManifest) string {
	return filepath.Join(unitDir, m.MediaTarFileName)
}

// ExtractMediaTar extracts the tarball into the live media directory
// (<dataDir>/media). It is the post-activation half of the coordinated
// restore (extraction runs after database activation, before the
// restore marker is removed).
//
// # Safety
//
// Only regular files are accepted; entry names must be clean,
// relative, and free of "."/".." components (no absolute paths, no
// traversal); per-file and total sizes are bounded; each entry is limited to
// its declared size. Files present in the live media directory but absent
// from the tar are left alone — media added after the backup point becomes
// unreferenced, which is the graceful outcome the sweep later resolves.
func ExtractMediaTar(dataDir, tarPath string) error {
	mediaRoot := filepath.Join(dataDir, "media")
	f, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("media: open tarball: %w", err)
	}
	defer f.Close()

	tr := tar.NewReader(f)
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("media: read tarball entry: %w", err)
		}
		name := filepath.FromSlash(hdr.Name)
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("media: tarball entry %q is not a regular file", hdr.Name)
		}
		if !safeExtractName(name) {
			return fmt.Errorf("media: unsafe tarball entry name %q", hdr.Name)
		}
		if hdr.Size > maxExtractFileBytes {
			return fmt.Errorf("media: tarball entry %q exceeds the per-file limit (%d > %d bytes)", hdr.Name, hdr.Size, maxExtractFileBytes)
		}
		total += hdr.Size
		if total > maxExtractTotalBytes {
			return fmt.Errorf("media: tarball exceeds the total extraction limit (%d bytes)", maxExtractTotalBytes)
		}

		dest := filepath.Join(mediaRoot, name)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("media: create extraction directory: %w", err)
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("media: create extraction file %q: %v", hdr.Name, err)
		}
		_, copyErr := io.Copy(out, io.LimitReader(tr, hdr.Size))
		closeErr := out.Close()
		if copyErr != nil {
			return fmt.Errorf("media: extract %q: %v", hdr.Name, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("media: close extracted file %q: %v", hdr.Name, closeErr)
		}
	}
}

// safeExtractName rejects any entry name that could escape the media root:
// absolute paths, backslash-separated names, unclean paths (a/b/../c, ./x,
// doubled separators), and "."/".." components.
func safeExtractName(name string) bool {
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return false
	}
	if name != path.Clean(name) {
		return false
	}
	for part := range strings.SplitSeq(name, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

// randomSuffix returns 8 random hex characters for unpredictable temp names
// (mirrors the database package's convention; kept local to avoid a
// cross-package helper).
func randomSuffix() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
