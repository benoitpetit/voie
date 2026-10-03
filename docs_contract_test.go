package main

import (
	"os"
	"strings"
	"testing"
)

// readDoc reads a documentation file relative to the repository root.
func readDoc(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func assertContains(t *testing.T, label, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("%s: missing %q", label, needle)
	}
}

func assertOmits(t *testing.T, label, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("%s: must not contain %q", label, needle)
	}
}

func TestLicenceAndReleaseLink(t *testing.T) {
	assertContains(t, "LICENSE", readDoc(t, "LICENSE"), "MIT License")
	assertOmits(t, "README.md", readDoc(t, "README.md"), "releases/tag/v0.0.3")
}

func TestReadmeFramesVoieAsRelay(t *testing.T) {
	readme := readDoc(t, "README.md")
	assertOmits(t, "README.md", readme, "to discover and call supported models")
	assertContains(t, "README.md", readme, "relays requests to public model providers")
	assertContains(t, "README.md", readme, "does not run models")
}
