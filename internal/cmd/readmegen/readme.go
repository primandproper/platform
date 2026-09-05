package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Every generated region is delimited by an HTML comment pair, which markdown
// renders as nothing and which `git diff` shows as the boundary of what a hand
// edit will lose.
const (
	beginMarker = "<!-- readmegen:%s -->\n"
	endMarker   = "<!-- /readmegen:%s -->"
)

// rewrite reads the README at path, replaces every generated region, and writes
// it back. It reports whether the file changed, so a generate that finds the
// tree already described leaves the file's mtime alone.
func rewrite(path string, regions map[string]string) (bool, error) {
	clean := filepath.Clean(path)

	body, err := os.ReadFile(clean)
	if err != nil {
		return false, err
	}

	updated, err := splice(string(body), regions)
	if err != nil {
		return false, fmt.Errorf("%s: %w", clean, err)
	}

	if updated == string(body) {
		return false, nil
	}

	//nolint:gosec // G703: the path is this command's -out flag joined onto its -root, which is the file it exists to write.
	return true, os.WriteFile(clean, []byte(updated), 0o600)
}

// splice replaces the body of every named region, and fails on a README that is
// missing one. A region whose markers have been deleted is a section that has
// stopped being generated without anybody saying so, and emitting the other two
// while quietly skipping it is the failure mode this whole change is here to
// remove.
func splice(body string, regions map[string]string) (string, error) {
	for _, name := range slices.Sorted(maps.Keys(regions)) {
		var err error

		body, err = replaceRegion(body, name, regions[name])
		if err != nil {
			return "", err
		}
	}

	return body, nil
}

func replaceRegion(body, name, content string) (string, error) {
	begin := fmt.Sprintf(beginMarker, name)
	end := fmt.Sprintf(endMarker, name)

	open := strings.Index(body, begin)
	if open < 0 {
		return "", fmt.Errorf("no %q marker", strings.TrimSuffix(begin, "\n"))
	}

	start := open + len(begin)

	closed := strings.Index(body[start:], end)
	if closed < 0 {
		return "", fmt.Errorf("%q is not closed by %q", strings.TrimSuffix(begin, "\n"), end)
	}

	return body[:start] + content + body[start+closed:], nil
}
