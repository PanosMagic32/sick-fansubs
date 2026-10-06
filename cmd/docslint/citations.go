package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// citationPattern matches every reference to a numbered decision record: the
// prose forms "decision NNNN", "decision/NNNN", "decision-NNNN", and a record
// path. The records are retired, so no living file may carry a reference.
var citationPattern = regexp.MustCompile(`(?i)\bdecisions?[ /-]\d{4}`)

// citationExemptDir is the one recorded path exemption: applied migration
// files are checksummed history and cannot be edited, so a comment inside them
// may cite a decision record forever.
const citationExemptDir = "internal/database/migrations"

// citationExtensions lists the file kinds the citation check counts.
var citationExtensions = map[string]bool{
	".go": true, ".md": true, ".ts": true, ".css": true, ".json": true,
	".yaml": true, ".yml": true, ".sh": true, ".sql": true, ".html": true,
	".js": true, ".toml": true,
}

// citationNames lists extensionless files that carry citations: build,
// environment, and shipped-config files where a reference is just as real as
// one in Go source.
var citationNames = map[string]bool{
	"Makefile": true, "Caddyfile": true, ".gitignore": true, ".env.example": true,
	"logrotate.sick-fansubs": true,
}

// checkCitations fails every counted file that still cites a decision record,
// except the checksummed migrations under citationExemptDir.
func checkCitations(root string) ([]string, error) {
	counts, err := countCitations(root)
	if err != nil {
		return nil, err
	}
	var problems []string
	for path, n := range counts {
		if strings.HasPrefix(path, citationExemptDir+"/") {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"%s: %s to a retired decision record — move the fact into a pattern or feature doc",
			path, plural(n, "reference")))
	}
	slices.Sort(problems)
	return problems, nil
}

// countCitations counts matches per file, skipping this command's test files,
// unlisted file kinds, and the skipped directories.
func countCitations(root string) (map[string]int, error) {
	counts := map[string]int{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(relTo(root, path))
		if d.IsDir() {
			if nestedSkipNames[d.Name()] || rootSkips[rel] {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(rel, checkerDir+"/") && strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		if !isCitationFile(rel) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if n := len(citationPattern.FindAll(body, -1)); n > 0 {
			counts[rel] = n
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return counts, nil
}

// isCitationFile reports whether a file's kind is counted by the citation check.
func isCitationFile(rel string) bool {
	base := filepath.Base(rel)
	if citationExtensions[strings.ToLower(filepath.Ext(base))] {
		return true
	}
	return citationNames[base] || strings.HasPrefix(base, "Dockerfile")
}
