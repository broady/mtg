package tappedout

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type Deck struct {
	Mainboard    []*Entry
	Sideboard    []*Entry
	Maybeboard   []*Entry
	Acquireboard []*Entry
	Commanders   []*Entry
}

type Entry struct {
	Quantity            int
	CardName            string
	Printing            string
	Foil, Alter, Signed bool

	Commander bool
}

var markdownRE = regexp.MustCompile(`\[([^]]*)\]`)

// always returns a 401. need an API key.
func deckFromURLWithAPIKey(deckURL string) (*Deck, error) {
	u, err := url.Parse(deckURL)
	if err != nil {
		return nil, err
	}
	if u.Host != "tappedout.net" && u.Host != "www.tappedout.net" {
		return nil, fmt.Errorf("must be a tappedout.net URL; got %q", u.Host)
	}
	if !strings.HasPrefix(u.Path, "/mtg-decks/") {
		return nil, errors.New("must be a deck URL")
	}
	slug := u.Path[len("/mtg-decks/"):]

	// NOTE: tappedout is horrible and redirects https to http incorrectly.
	req, _ := http.NewRequest("GET", "http://tappedout.net/api/collection:deck/"+slug, nil)
	req.Header.Set("User-Agent", "github.com_broady_mtg")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode > 299 {
		return nil, fmt.Errorf("%q: non-OK response from tappedout: %s", slug, resp.Status)
	}

	deck := &Deck{}

	var out struct {
		Inventory struct {
			Cards [][]json.RawMessage
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	for _, card := range out.Inventory.Cards {
		var name string
		if err := json.Unmarshal(card[0], &name); err != nil {
			return nil, fmt.Errorf("could not unmarshal name: %v (%s)", err, card)
		}
		var info struct {
			Qty    int
			B      string
			TLA    string
			Alter  bool
			Foil   bool
			Signed bool
			Cmdr   bool
		}
		if err := json.Unmarshal(card[1], &info); err != nil {
			return nil, fmt.Errorf("could not unmarshal info: %v (%s)", err, card)
		}

		entry := &Entry{
			Quantity:  info.Qty,
			Printing:  info.TLA,
			Alter:     info.Alter,
			Foil:      info.Foil,
			Signed:    info.Signed,
			Commander: info.Cmdr,
		}
		switch info.B {
		case "main":
			deck.Mainboard = append(deck.Mainboard, entry)
			if entry.Commander {
				deck.Commanders = append(deck.Commanders, entry)
			}
		case "side":
			deck.Sideboard = append(deck.Sideboard, entry)
		case "maybe":
			deck.Maybeboard = append(deck.Maybeboard, entry)
		case "acquire":
			deck.Acquireboard = append(deck.Acquireboard, entry)
		default:
			return nil, fmt.Errorf("bad board: %s", card)
		}
	}

	return deck, nil
}

func DeckFromURL(deckURL string) (*Deck, error) {
	u, err := url.Parse(deckURL)
	if err != nil {
		return nil, err
	}
	if u.Host != "tappedout.net" && u.Host != "www.tappedout.net" {
		return nil, fmt.Errorf("must be a tappedout.net URL; got %q", u.Host)
	}
	if !strings.HasPrefix(u.Path, "/mtg-decks/") {
		return nil, errors.New("must be a deck URL")
	}

	// NOTE: tappedout is horrible and redirects https to http incorrectly.
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://tappedout.net%s?fmt=csv", u.Path), nil)
	req.Header.Set("User-Agent", "github.com_broady_mtg")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode > 299 {
		return nil, fmt.Errorf("%q: non-OK response from tappedout: %s", u.Path, resp.Status)
	}

	rows, err := csvToMapSlice(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("empty deck")
	}
	for _, header := range []string{"Board", "Qty", "Name", "Printing", "Foil", "Alter", "Signed", "Condition", "Languange"} {
		if rows[0][header] != header {
			return nil, fmt.Errorf("unknown formatting in tappedout response. missing header %q", header)
		}
	}

	deck := &Deck{}

	for _, row := range rows[1:] {
		qty, err := strconv.Atoi(row["Qty"])
		if err != nil {
			return nil, fmt.Errorf("bad quantity: %+v", row)
		}

		entry := &Entry{
			Quantity: qty,
			CardName: row["Name"],
			Printing: row["Printing"],
			Foil:     row["Foil"] != "",
			Alter:    row["Alter"] != "",
			Signed:   row["Signed"] != "",
		}

		switch row["Board"] {
		case "main":
			deck.Mainboard = append(deck.Mainboard, entry)
		case "side":
			deck.Sideboard = append(deck.Sideboard, entry)
		case "maybe":
			deck.Maybeboard = append(deck.Maybeboard, entry)
		case "acquire":
			deck.Acquireboard = append(deck.Acquireboard, entry)
		default:
			return nil, fmt.Errorf("bad board: %+v", row)
		}
	}

	req, _ = http.NewRequest("GET", fmt.Sprintf("http://tappedout.net%s?fmt=markdown", u.Path), nil)
	req.Header.Set("User-Agent", "github.com_broady_mtg")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode > 299 {
		return nil, fmt.Errorf("%q: non-OK response from tappedout: %s", u.Path, resp.Status)
	}
	scanner := bufio.NewScanner(resp.Body)
	commanders := map[string]bool{}
outer:
	for scanner.Scan() {
		l := scanner.Text()
		if strings.HasPrefix(l, "#") && strings.Contains(l, "Commander") {
			for scanner.Scan() {
				l := scanner.Text()
				parts := markdownRE.FindStringSubmatch(l)
				if len(parts) == 2 {
					commanders[parts[1]] = true
				}
				if strings.HasPrefix(l, "#") {
					// We're in the next section
					break outer
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	for _, entry := range deck.Mainboard {
		if commanders[entry.CardName] {
			entry.Commander = true
			deck.Commanders = append(deck.Commanders, entry)
		}
	}

	return deck, nil
}

func csvToMapSlice(r io.Reader) ([]map[string]string, error) {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	rows, err := csv.NewReader(bytes.NewReader(b)).ReadAll()
	if err != nil {
		if len(b) > 100 {
			return nil, fmt.Errorf("csv.Read: %v; \n%s <snip>", err, b[:99])
		}
		return nil, fmt.Errorf("csv.Read: %v; \n%s", err, b)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	var resp []map[string]string
	for _, row := range rows {
		mapped := map[string]string{}
		for i, cell := range row {
			mapped[rows[0][i]] = cell
		}
		resp = append(resp, mapped)
	}
	return resp, nil
}
