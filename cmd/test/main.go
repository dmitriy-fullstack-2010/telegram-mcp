package main

import (
	"context"
	"os"
	"os/signal"
	"strconv"

	"github.com/dmitriy-fullstack-2010/telegram-mcp/internal/tg"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: zerolog.TimeFormatUnix,
	})

	appIDStr, apiHash, sessionPath := os.Getenv("TG_APP_ID"), os.Getenv("TG_API_HASH"), os.Getenv("TG_SESSION_PATH")
	if appIDStr == "" {
		log.Fatal().Msg("TG_APP_ID is required")
	}
	if apiHash == "" {
		log.Fatal().Msg("TG_API_HASH is required")
	}
	if sessionPath == "" {
		log.Fatal().Msg("TG_SESSION_PATH is required")
	}

	appID, err := strconv.Atoi(appIDStr)
	if err != nil {
		log.Fatal().Err(err).Msg("TG_APP_ID app id")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	client := tg.New(appID, apiHash, sessionPath).T()

	if err := client.Run(ctx, func(ctx context.Context) error {
		self, err := client.Self(ctx)
		if err != nil {
			return errors.Wrap(err, "get self")
		}

		log.Info().
			Str("first_name", self.FirstName).
			Str("last_name", self.LastName).
			Str("username", self.Username).
			Int64("id", self.ID).
			Msg("Logged in as")

		messages, err := getUnreadMessages(ctx, client)
		if err != nil {
			return errors.Wrap(err, "get unread messages")
		}

		for _, msg := range messages {
			log.Info().
				Int("id", msg.ID).
				Str("text", msg.Text).
				Time("date", msg.Date).
				Int64("from_id", msg.FromID).
				Str("from_name", msg.FromName).
				Str("chat_type", msg.ChatType).
				Str("chat_title", msg.ChatTitle).
				Msg("Unread message")
		}

		return nil
	}); err != nil {
		log.Fatal().Err(err).Msg("client error")
	}
}
