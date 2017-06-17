// Package cards provides an interface to fetch card data from mtgjson.com
package cards

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type Card struct {
	Name          string
	Names         []string
	ManaCost      string
	CMC           float64
	Colors        []string
	ColorIdentity []string
	Type          string
	SuperTypes    []string
	Types         []string
	SubTypes      []string
	Rarity        string
	Text          string
	Flavor        string
	Power         string
	Toughness     string
	Printings     []string
	Legalities    []FormatLegality
	Rulings       []Ruling
	// Loyalty       int // Nissa has "X".
	// Only relevant for specific sets.
	// MultiverseID  int
}

type Ruling struct {
	Date string
	Text string
}

type FormatLegality struct {
	Format   string
	Legality string
}

func NewStore() *Store {
	s := &Store{
		Logger:          log.New(os.Stderr, "cards.Store: ", log.LstdFlags),
		updateFrequency: time.Hour,
		ready:           make(chan bool),
		closed:          make(chan bool),
		notifyCh:        make(chan bool),
	}
	go s.watch()
	return s
}

// Cards is a corpus of cards.
//
// In the future, it will have some methods to allow for search.
type Cards struct {
	// Map from card name to the card.
	M map[string]*Card

	// Map from normalized card name to the card.
	normalized map[string]*Card
}

// LookupNormalized looks up a card name, ignoring case and other
// symbols (i.e., "Beck // Call" is equivalent to "beck & CALL")
func (c *Cards) LookupNormalized(cardName string) *Card {
	return c.normalized[strings.ToLower(normalizeCardName(cardName))]
}

func (c *Cards) generateNormalized() {
	for _, card := range c.M {
		if len(card.Names) != 0 {
			c.normalized[strings.ToLower(strings.Join(card.Names, " & "))] = card
			c.normalized[strings.ToLower(strings.Join(card.Names, " / "))] = card
			c.normalized[strings.ToLower(strings.Join(card.Names, " // "))] = card
		}
		c.normalized[strings.ToLower(normalizeCardName(card.Name))] = card
	}
}

func normalizeCardName(s string) string {
	s = strings.Replace(s, "Æ", "Ae", -1)
	return strings.Replace(s, "’", "'", -1)
}

// Store is a card store that periodically updates itself from mtgjson.com.
type Store struct {
	// If set, messages from the auto-updater are logged.
	// Default is to log to stderr.
	Logger *log.Logger

	// Used to perform the updates. If unset, http.DefaultClient is used.
	Client *http.Client

	updateFrequency time.Duration
	closed          chan bool
	ready           chan bool

	mu       sync.RWMutex
	cards    *Cards
	etag     string
	notifyCh chan bool
}

// Close prevents future updates.
func (s *Store) Close() error {
	select {
	case <-s.closed:
		return errors.New("already closed")
	default:
		close(s.closed)
	}
	return nil
}

// WaitForUpdate returns a channel that is closed when an update is performed.
//
// Sample usage:
//
//    s := cards.NewStore()
//    s.Cards()
//    // Perform some indexing on cards.
//    for {
//    	cards := <-s.WaitForUpdate()
//    	// Perform some re-indexing on cards.
//    }
func (s *Store) WaitForUpdate() <-chan *Cards {
	s.mu.Lock()
	notifyCh := s.notifyCh
	s.mu.Unlock()
	ch := make(chan *Cards)
	go func() {
		<-notifyCh
		ch <- s.Cards()
		close(ch)
	}()
	return ch
}

func (s *Store) watch() {
	s.maybeUpdate()
	for {
		if s.updateFrequency == 0 {
			return
		}
		select {
		case <-time.After(s.updateFrequency):
		case <-s.closed:
			return
		}
		s.maybeUpdate()
	}
}

var nowhereLogger = log.New(ioutil.Discard, "", 0)

func (s *Store) log() *log.Logger {
	logger := s.Logger
	if logger == nil {
		return nowhereLogger
	}
	return logger
}

func (s *Store) maybeUpdate() {
	s.mu.Lock()
	etag := s.etag
	s.mu.Unlock()

	req, _ := http.NewRequest("GET", "https://mtgjson.com/json/AllCards-x.json", nil)
	req.Header.Set("If-None-Match", etag)
	req.Header.Set("User-Agent", "github.com_broady_mtg")

	hc := s.Client
	if hc == nil {
		hc = http.DefaultClient
	}

	resp, err := hc.Do(req)
	if err != nil {
		s.log().Printf("Could not update: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return
	}
	b, rerr := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		s.log().Printf("Card update failed - HTTP %d, body:\n---\n%s\n---", resp.StatusCode, truncate(b, 1000))
		return
	}
	if rerr != nil {
		s.log().Printf("Could not read body: %v", rerr)
		return
	}

	cards := &Cards{
		M:          make(map[string]*Card),
		normalized: make(map[string]*Card),
	}
	if err := json.Unmarshal(b, &cards.M); err != nil {
		s.log().Printf("Could not unmarshal cards: %v, body:\n---\n%s\n---", err, truncate(b, 1000))
		return
	}
	cards.generateNormalized()
	s.mu.Lock()
	s.etag = resp.Header.Get("Etag")
	s.cards = cards
	s.notifyCh = make(chan bool)
	notify := s.notifyCh
	s.mu.Unlock()

	// Notify.
	close(notify)

	select {
	case <-s.ready:
	default:
		close(s.ready)
	}

	s.log().Printf("Card update successful")
}

func truncate(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}

func (s *Store) Cards() *Cards {
	<-s.ready

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.cards
}
