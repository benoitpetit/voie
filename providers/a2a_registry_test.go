package providers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/benoitpetit/voie/config"
)

func a2aCardServer(t *testing.T, name string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"` + name + `","description":"remote","supportedInterfaces":[{"url":"http://example/a2a","protocolBinding":"JSONRPC","protocolVersion":"1.0"}],"capabilities":{"streaming":true},"skills":[{"id":"voie.chat.classic","name":"Classic","description":"chat","inputModes":["text/plain"],"outputModes":["text/plain"]}],"defaultInputModes":["text/plain"],"defaultOutputModes":["text/plain"]}`))
	}))
}

func TestConnectA2ACachesCardAndExposesSkillModels(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	server := a2aCardServer(t, "one")
	defer server.Close()
	store, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := ConnectA2A(store, "one", server.URL, "tok"); err != nil {
		t.Fatal(err)
	}
	if got := store.A2AAgentURL("one"); got != server.URL {
		t.Fatalf("URL = %q", got)
	}
	ids := A2AModelIDs("one")
	if len(ids) != 1 || ids[0] != "a2a-one-voie.chat.classic" {
		t.Fatalf("models = %v", ids)
	}
	if _, err := os.Stat(filepath.Join(A2ACardDir(), "one.json")); err != nil {
		t.Fatalf("card cache missing: %v", err)
	}
}

func TestA2AModelIDsAreDistinctPerAgent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	server := a2aCardServer(t, "same")
	defer server.Close()
	store, _ := config.LoadCredentials()
	if err := ConnectA2A(store, "one", server.URL, "tok"); err != nil {
		t.Fatal(err)
	}
	if err := ConnectA2A(store, "two", server.URL, "tok"); err != nil {
		t.Fatal(err)
	}
	one, two := A2AModelIDs("one"), A2AModelIDs("two")
	if len(one) != 1 || len(two) != 1 || one[0] == two[0] {
		t.Fatalf("model IDs collide: %v / %v", one, two)
	}
}
