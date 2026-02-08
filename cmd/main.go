package main

import (
	"context"
	"database/sql"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"example.com/random_coffee/pkg/logger"
	"github.com/NicoNex/echotron/v3"
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	_ "modernc.org/sqlite"
)

type Bot struct {
	echotron.API
	DB     *sql.DB
	mu     sync.Mutex
	ChatID int64
}

// dualFormatWriter writes JSON logs to jsonWriter and parses them for consoleWriter
type dualFormatWriter struct {
	console io.Writer
	json    io.Writer
}

func (w *dualFormatWriter) Write(p []byte) (n int, err error) {
	// Send JSON to admin notifier
	w.json.Write(p)

	// Send to console writer
	return w.console.Write(p)
}

func recoverPanic(contextFields map[string]any) {
	if r := recover(); r != nil {
		entry := log.Error().Interface("panic", r)
		for k, v := range contextFields {
			entry = entry.Interface(k, v)
		}
	}
}

func (b *Bot) Update(u *echotron.Update) {
	b.mu.Lock()
	defer b.mu.Unlock()
	defer recoverPanic(map[string]any{"handler": "Update"})

	ctx := context.Background()

	if u.PollAnswer != nil {
		HandlePollAnswer(ctx, b.DB, b.API, u.PollAnswer)
		return
	}

	if u.Message != nil {
		if u.Message.Chat.Type == "private" {
			HandlePrivateCommand(ctx, b.DB, b.API, u.Message)
			return
		}
		HandleGroupCommand(ctx, b.DB, b.API, u.Message)
	}

}

func main() {
	logger.Init(logger.Config{
		PrettyConsole: true,
	})
	log.Info().Msg("Starting bot...")

	botToken := mustEnv("TELEGRAM__TOKEN")
	dbPath := mustEnv("DB__URL")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal().Err(err).Msg("sql.Open failed")
	}
	defer func() { _ = db.Close() }()

	err = runMigrations(db)
	if err != nil {
		log.Fatal().Err(err).Msg("runMigrations failed")
	}

	initAdmins()

	botAPI := echotron.NewAPI(botToken)

	if len(adminChatIDsMap) > 0 {
		// Setup dual logger: console (pretty) + admin notifier (JSON)
		consoleWriter := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"}
		jsonWriter := NewAdminNotifier(botAPI, adminChatIDsMap, io.Discard)

		// Create a custom writer that duplicates to both console and JSON
		multiWriter := &dualFormatWriter{
			console: consoleWriter,
			json:    jsonWriter,
		}

		log.Logger = zerolog.New(multiWriter).With().Timestamp().Logger()
		log.Info().Msg("Admin notifier enabled")
	}

	stop := make(chan struct{})
	startScheduler(db, botAPI, stop)

	newBot := func(chatID int64) echotron.Bot { return &Bot{ChatID: chatID, DB: db, API: echotron.NewAPI(botToken)} }

	dsp := echotron.NewDispatcher(botToken, newBot)

	updateOpts := echotron.UpdateOptions{
		// AllowedUpdates: []echotron.UpdateType{
		// 	echotron.MessageUpdate,
		// 	echotron.CallbackQueryUpdate,
		// 	echotron.PollAnswerUpdate,
		// },
	}

	errChan := make(chan error, 1)
	go func() {
		defer recoverPanic(map[string]any{"handler": "polling"})

		log.Info().Msg("Bot polling started")
		for {
			if err := dsp.PollOptions(false, updateOpts); err != nil {
				log.Error().Err(err).Msg("dsp.Poll failed, retrying in 5 seconds...")
				time.Sleep(5 * time.Second)
				continue
			}
			break
		}
		errChan <- nil
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
		log.Info().Msg("Received shutdown signal")
	case err := <-errChan:
		if err != nil {
			log.Error().Err(err).Msg("Bot polling failed")
		}
	}

	log.Info().Msg("Shutting down gracefully...")
	close(stop)
	time.Sleep(1 * time.Second)
	log.Info().Msg("Goodbye!")
}

func runMigrations(db *sql.DB) error {
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}

	if err := goose.Up(db, "migrations"); err != nil {
		return err
	}

	log.Info().Msg("Migrations applied successfully")
	return nil
}

func scheduleJob(jobName string, weekday time.Weekday, hour, minute int,
	jobFunc func(context.Context, *sql.DB, echotron.API), db *sql.DB, api echotron.API, stopChan chan struct{}, location *time.Location) {

	go func() {
		defer recoverPanic(map[string]any{"handler": "scheduler", "job": jobName})

		for {
			now := time.Now().In(location)
			next := nextOccurrence(now, weekday, hour, minute, location)
			duration := next.Sub(now)

			log.Info().Str("job", jobName).Time("next_run", next).Dur("in", duration).Msg("Scheduled")

			select {
			case <-time.After(duration):
				log.Info().Str("job", jobName).Msg("Running scheduled job")
				ctx := context.Background()
				jobFunc(ctx, db, api)
			case <-stopChan:
				log.Info().Str("job", jobName).Msg("Job stopped")
				return
			}
		}
	}()
}

func startScheduler(db *sql.DB, api echotron.API, stopChan chan struct{}) {
	moscowTZ, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load Europe/Moscow timezone")
	}

	// Friday 17:00 - send quiz
	scheduleJob("send_quiz", time.Friday, 17, 0, SendQuizToAllGroups, db, api, stopChan, moscowTZ)

	scheduleJob("send_quiz", time.Wednesday, 16, 19, SendQuizToAllGroups, db, api, stopChan, moscowTZ)

	// Sunday 19:00 - create pairs
	scheduleJob("create_pairs", time.Sunday, 19, 0, CreatePairsForAllGroups, db, api, stopChan, moscowTZ)

	log.Info().Msg("Scheduler started")
}

func nextOccurrence(now time.Time, weekday time.Weekday, hour, minute int, location *time.Location) time.Time {
	target := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, location)

	if now.Weekday() == weekday && now.Before(target) {
		return target
	}

	daysUntil := int(weekday - now.Weekday())
	if daysUntil <= 0 {
		daysUntil += 7
	}

	return target.AddDate(0, 0, daysUntil)
}

func mustEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatal().Str("env", key).Msg("missing required environment variable")
	}
	return value
}
