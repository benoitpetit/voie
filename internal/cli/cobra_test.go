package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRootHelpListsCommandsWithoutInitializingRuntime(t *testing.T) {
	var stdout, stderr bytes.Buffer
	initialized := false
	err := Execute(context.Background(), nil, nil, &stdout, &stderr, func() (Runtime, error) {
		initialized = true
		return nil, nil
	})
	if err != nil {
		t.Fatalf("execute root help: %v", err)
	}
	for _, expected := range []string{"Usage:", "chat", "mcp", "models", "providers", "serve", "help"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("root help missing %q:\n%s", expected, stdout.String())
		}
	}
	if initialized {
		t.Fatal("root help initialized application runtime")
	}
	if strings.Contains(stdout.String(), "\n  completion ") {
		t.Fatalf("unexpected unconfigured completion command in help:\n%s", stdout.String())
	}
}

func TestSubcommandHelpShowsOwnFlagsWithoutInitializingRuntime(t *testing.T) {
	for _, tc := range []struct{ command, flag string }{
		{"chat", "--model"}, {"serve", "--host"}, {"models", "--json"}, {"providers", "--json"}, {"mcp", "Usage:"},
	} {
		t.Run(tc.command, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			initialized := false
			if err := Execute(context.Background(), []string{tc.command, "--help"}, nil, &stdout, &stderr, func() (Runtime, error) {
				initialized = true
				return nil, nil
			}); err != nil {
				t.Fatalf("execute help: %v", err)
			}
			if !strings.Contains(stdout.String(), tc.flag) {
				t.Fatalf("%s help missing %q:\n%s", tc.command, tc.flag, stdout.String())
			}
			if initialized {
				t.Fatal("help initialized application runtime")
			}
		})
	}
}
