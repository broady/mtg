package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/broady/conf"
	"github.com/broady/mtg/cards"
	"github.com/davecgh/go-spew/spew"
	tg "github.com/go-telegram-bot-api/telegram-bot-api"
)

var verbose = flag.Bool("v", false, "verbose")

func main() {
	rand.Seed(time.Now().UnixNano())
	flag.Parse()

	tok := conf.MustGet(conf.Env("TELEGRAM_TOKEN"))
	b, err := tg.NewBotAPI(tok)
	fatal(err)

	bot := &mtgBot{b: b, store: cards.NewStore()}
	fatal(bot.Start())
}

type mtgBot struct {
	b     *tg.BotAPI
	store *cards.Store
}

func (bot *mtgBot) Start() error {
	bot.store = cards.NewStore()

	updates, err := bot.b.GetUpdatesChan(tg.UpdateConfig{Timeout: 60})
	if err != nil {
		return err
	}

	vlog("Got update chan. Waiting for updates.")

	for {
		u := <-updates

		switch {
		case u.InlineQuery != nil:
			go bot.handleInline(u.UpdateID, u.InlineQuery)
		default:
			vlog("unhandled")
			vlog(u)
		}
	}
}
func (bot *mtgBot) handleInline(id int, q *tg.InlineQuery) {
	var reply tg.InlineConfig
	reply.InlineQueryID = q.ID

	if q.Query == "" {
		if _, err := bot.b.AnswerInlineQuery(reply); err != nil {
			vlog(err)
		}
		return
	}

	cards, err := bot.store.Cards().Query(q.Query)
	if err != nil {
		vlog(err)
		return
	}

	for _, c := range cards {
		if len(reply.Results) > 10 {
			break
		}

		title := fmt.Sprintf("%s %v", c.Name, c.Types)
		txt := fmt.Sprintf(`*%s* %s
%s
https://api.scryfall.com/cards/named/?exact=%s&format=image`,
			c.Name, c.ManaCost,
			c.Text, url.QueryEscape(c.Name))

		res := tg.NewInlineQueryResultArticle(c.Name, title, "")
		res.Description = c.Text
		const maxDescription = 100
		if len(res.Description) > maxDescription {
			res.Description = res.Description[:maxDescription-3] + "..."
		}
		res.InputMessageContent = tg.InputTextMessageContent{
			Text:      txt,
			ParseMode: tg.ModeMarkdown,
		}

		reply.Results = append(reply.Results, res)
	}

	if _, err := bot.b.AnswerInlineQuery(reply); err != nil {
		vlog(err)
	}
}

func fatal(err error) {
	if err == nil {
		return
	}
	vlog(err)
	log.Fatal(err)
}

func vlog(v interface{}) {
	if !*verbose {
		return
	}
	var s string
	switch v.(type) {
	case string:
		s = v.(string)
	default:
		s = spew.Sdump(v)
	}
	_, f, l, ok := runtime.Caller(1)
	if ok {
		f = filepath.Base(f)
		log.Printf("%s:%d: %v", f, l, s)
	} else {
		log.Print(s)
	}
}

func closeOnTerm(c io.Closer) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		log.Printf("shutting down...")
		if err := c.Close(); err != nil {
			log.Fatalf("clean up error: %v", err)
		}
		os.Exit(1)
	}()
}
