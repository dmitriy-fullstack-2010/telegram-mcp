package main

import (
	"context"

	"github.com/dmitriy-fullstack-2010/telegram-mcp/internal/tg"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

func authCommand(_ context.Context, cmd *cli.Command) error {
	phone := cmd.String("phone")
	newSession := cmd.Bool("new")
	pass := cmd.String("password")
	appID := cmd.Root().Int("app-id")
	apiHash := cmd.Root().String("api-hash")
	sessionPath := cmd.Root().String("session")

	log.Info().
		Str("phone", phone).
		Str("session", sessionPath).
		Int64("app-id", appID).
		Msg("Authenticate with Telegram")

	err := tg.Auth(phone, appID, apiHash, sessionPath, pass, newSession)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to authenticate with Telegram")
	}

	log.Info().Str("session", sessionPath).Msg("Successfully authenticated with Telegram")

	return nil
}
