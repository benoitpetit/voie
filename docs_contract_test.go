package main

import (
	"os"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/runtime"
	"github.com/benoitpetit/voie/providers"
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

func TestInterfaceDocsDiscloseRelay(t *testing.T) {
	for _, tc := range []struct{ file, needle string }{
		{"api-reference.md", "no local inference"},
		{"docs/architecture.md", "no local inference"},
		{"docs/mcp.md", "no local inference"},
		{"skills/voie/SKILL.md", "no local inference"},
	} {
		assertContains(t, tc.file, readDoc(t, tc.file), tc.needle)
	}
}

func TestDocumentedCountsMatchCode(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	rt, err := runtime.NewWithRegistry(cfg, providers.NewRegistry())
	if err != nil {
		t.Fatalf("runtime.NewWithRegistry: %v", err)
	}

	names := rt.Registry.GetProviderNames()
	if len(names) != 7 {
		t.Errorf("registered providers = %d, want 7", len(names))
	}

	declared := 0
	for _, name := range names {
		declared += len(rt.Registry.Get(name).GetInfo().SupportedModels)
	}
	if declared != 52 {
		t.Errorf("declared model IDs = %d, want 52", declared)
	}

	if resolved := len(rt.AppService.ListModels()); resolved != 52 {
		t.Errorf("resolved model IDs = %d, want 52", resolved)
	}
}

func TestDocumentedCommandsAndStatusCodes(t *testing.T) {
	readme := readDoc(t, "README.md")
	for _, cmd := range []string{"serve", "chat", "conversations", "models", "providers", "mcp", "completion", "version", "update"} {
		assertContains(t, "README.md", readme, "`"+cmd+"`")
	}

	api := readDoc(t, "api-reference.md")
	for _, code := range []string{"400", "401", "404", "408", "409", "410", "500", "502", "503", "504"} {
		assertContains(t, "api-reference.md", api, "| `"+code+"` |")
	}
}
