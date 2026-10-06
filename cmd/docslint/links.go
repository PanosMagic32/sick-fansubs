package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// linkPatterns match inline markdown links and link definitions.
var (
	inlineLinkPattern = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)`)
	linkDefPattern    = regexp.MustCompile(`(?m)^[ \t]*\[[^\]]+\]:[ \t]*(\S+)`)
	// inlineCodePattern is stripped before link matching: a link cannot exist
	// inside a code span, and Go examples such as `f[T](arg)` look like one.
	inlineCodePattern = regexp.MustCompile("`[^`]*`")
)

// checkLinks resolves every relative markdown link in the durable docs and the
// feature docs. Fenced code blocks are ignored.
func checkLinks(root string) ([]string, error) {
	var problems []string
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
		if !strings.HasSuffix(path, ".md") || rel == docsIndex {
			return nil
		}
		broken, err := brokenLinks(root, path)
		if err != nil {
			return err
		}
		problems = append(problems, broken...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	// The index is checked explicitly so a missing index is an error rather
	// than a silent skip.
	broken, err := brokenLinks(root, filepath.Join(root, docsIndex))
	if err != nil {
		return nil, err
	}
	return append(problems, broken...), nil
}

// brokenLinks returns one message per link in path that does not resolve.
func brokenLinks(root, path string) ([]string, error) {
	rel := filepath.ToSlash(relTo(root, path))
	var problems []string
	err := eachLink(path, func(target string) {
		if !isRepoRelative(target) {
			return
		}
		clean, _, _ := strings.Cut(target, "#")
		clean, _, _ = strings.Cut(clean, "?")
		if clean == "" {
			return
		}
		candidate := filepath.Join(filepath.Dir(path), filepath.FromSlash(clean))
		if !exists(candidate) {
			problems = append(problems, fmt.Sprintf("%s: link does not resolve: %s", rel, target))
		}
	})
	if err != nil {
		return nil, err
	}
	return problems, nil
}

// linkTargets returns the set of files a markdown file links to, resolved
// against the file's own directory.
func linkTargets(path string) (map[string]bool, error) {
	targets := map[string]bool{}
	err := eachLink(path, func(target string) {
		if !isRepoRelative(target) {
			return
		}
		clean, _, _ := strings.Cut(target, "#")
		clean, _, _ = strings.Cut(clean, "?")
		if clean == "" {
			return
		}
		targets[filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(clean)))] = true
	})
	if err != nil {
		return nil, err
	}
	return targets, nil
}

// eachLink calls visit for every markdown link in the file, skipping fenced
// code blocks.
func eachLink(path string, visit func(target string)) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	inFence := false
	for line := range strings.SplitSeq(string(body), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		line = inlineCodePattern.ReplaceAllString(line, " ")
		matches := inlineLinkPattern.FindAllStringSubmatch(line, -1)
		matches = append(matches, linkDefPattern.FindAllStringSubmatch(line, -1)...)
		for _, match := range matches {
			visit(match[1])
		}
	}
	return nil
}

// isRepoRelative reports whether a link target is a path inside the repository
// that must resolve on disk.
func isRepoRelative(target string) bool {
	switch {
	case strings.HasPrefix(target, "#"):
		return false
	case strings.HasPrefix(target, "/"):
		return false
	case strings.Contains(target, "://"):
		return false
	case strings.HasPrefix(target, "mailto:"), strings.HasPrefix(target, "tel:"):
		return false
	}
	return true
}
