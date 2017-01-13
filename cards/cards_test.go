package cards

import "testing"

func TestGet(t *testing.T) {
	s := NewStore()
	cards := s.Cards()
	if len(cards.M) < 1000 {
		t.Fatal("got %d cards; want many", len(cards.M))
	}
	shock, ok := cards.M["Shock"]
	if shock.Type != "Instant" {
		t.Fatal("want Shock to be an instant")
	}
	if !ok {
		t.Fatal("couldn't find Shock")
	}
}
