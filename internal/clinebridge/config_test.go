package clinebridge

import (
	"context"
	"reflect"
	"testing"
)

func TestFreeModelsWithoutFeedUsesConfiguredModel(t *testing.T) {
	t.Setenv("VOIE_CLINE_FREE_FEED_URL", "")
	models, err := freeModelsFor(context.Background(), "", "cb-auto")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(models, []string{"cb-auto"}) {
		t.Fatalf("freeModels = %v, want [cb-auto] and no Cline feed fallback", models)
	}
}
