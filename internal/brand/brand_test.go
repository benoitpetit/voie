package brand

import "testing"

func TestBannerReturnsVOIEWordmark(t *testing.T) {
	want := "▄ ▄  ▄  ▄ ▄▄\n█ █ █ █ ▄ █■\n▀■▀  ▀  ▀ ▀▀"
	if got := Banner(); got != want {
		t.Fatalf("Banner() = %q, want %q", got, want)
	}
}
