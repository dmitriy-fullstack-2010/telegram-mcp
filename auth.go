package main

import (
	"context"
	"encoding/json"
	"strconv"

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
		Str("api-hash", apiHash).
		Str("session", sessionPath).
		Int64("app-id", appID).
		Msg("Authenticate with Telegram")

	err := tg.Auth(phone, appID, apiHash, sessionPath, pass, newSession)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to authenticate with Telegram")
	}

	c := struct {
		Telegram struct {
			Command string `json:"command"`
			Env     struct {
				AppID   string `json:"TG_APP_ID"`
				APIHash string `json:"TG_API_HASH"`
			} `json:"env"`
		} `json:"telegram"`
	}{
		Telegram: struct {
			Command string `json:"command"`
			Env     struct {
				AppID   string `json:"TG_APP_ID"`
				APIHash string `json:"TG_API_HASH"`
			} `json:"env"`
		}{
			Command: "telegram-mcp",
			Env: struct {
				AppID   string `json:"TG_APP_ID"`
				APIHash string `json:"TG_API_HASH"`
			}{
				AppID:   strconv.FormatInt(appID, 10),
				APIHash: apiHash,
			},
		},
	}

	data, _ := json.MarshalIndent(c, "", "\t")
	log.Info().RawJSON("config", data).Msg("Successfully authenticated with Telegram")

	return nil
}
