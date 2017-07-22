package tappedout

import "testing"

func TestMistform(t *testing.T) {
	deck, err := DeckFromURL("http://tappedout.net/mtg-decks/04-07-17-mistform-ultimus/")
	if err != nil {
		t.Fatal(err)
	}

	main := 0
	for _, e := range deck.Mainboard {
		main += e.Quantity
	}
	if main != 100 {
		t.Errorf("mainboard qty: got %d; want 100", main)
	}
}

func TestPartners(t *testing.T) {
	deck, err := DeckFromURL("http://tappedout.net/mtg-decks/22-07-17-test-deck/")
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range deck.Mainboard {
		if e.CardName == "Swamp" {
			continue
		}
		if !e.Commander {
			t.Errorf("got non-commander, want %q to be commander\n%+v", e.CardName, e)
		}
	}

	if len(deck.Commanders) != 2 {
		t.Errorf("got %d commanders; want 2\n%+v", len(deck.Commanders), deck)
	}
}

func TestNonCommander(t *testing.T) {
	_, err := DeckFromURL("http://tappedout.net/mtg-decks/10-11-14-Jzy-mardu-midrange/")
	if err != nil {
		t.Fatal(err)
	}
}
