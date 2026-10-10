package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

const (
	dir = ".telegram-mcp"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	debugPath := os.Getenv("TG_DEBUG_LOG")
	if debugPath != "" {
		logFile, err := os.OpenFile(debugPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to open debug log file")
		}

		log.Logger = log.Output(logFile)
		log.Info().Msgf("Enabling debug logging to %s", debugPath)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to get home dir")
	}

	configDir := filepath.Join(homeDir, dir)
	sesionPath := filepath.Join(configDir, "session.json")

	// Credentials (TG_APP_ID, TG_API_HASH) are read from this file so they
	// don't have to be stored in the MCP client config.
	envPath := os.Getenv("TG_ENV_FILE")
	if envPath == "" {
		envPath = filepath.Join(configDir, ".env")
	}

	if err := loadEnvFile(envPath); err != nil {
		log.Fatal().Err(err).Msg("Failed to load env file")
	}

	app := &cli.Command{
		Name:  "telegram-mcp",
		Usage: "Telegram MCP server",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:     "app-id",
				Usage:    "Telegram App ID",
				Required: true,
				Sources:  cli.EnvVars("TG_APP_ID"),
			},
			&cli.StringFlag{
				Name:     "api-hash",
				Usage:    "Telegram API Hash",
				Required: true,
				Sources:  cli.EnvVars("TG_API_HASH"),
			},
			&cli.StringFlag{
				Name:    "session",
				Usage:   "Path to session file",
				Value:   sesionPath,
				Sources: cli.EnvVars("TG_SESSION_PATH"),
			},
			&cli.StringFlag{
				Name:    "schema-version",
				Usage:   "JSON Schema version URL (e.g. https://json-schema.org/draft-07/schema#)",
				Sources: cli.EnvVars("TG_SCHEMA_VERSION"),
			},
			&cli.BoolFlag{
				Name:        "dry",
				Usage:       "Test configuration",
				Local:       true,
				HideDefault: true,
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "auth",
				Usage: "Authenticate with Telegram",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "phone",
						Usage:    "Phone number to authenticate with",
						Required: true,
						Aliases:  []string{"p"},
					},
					&cli.StringFlag{
						Name:        "password",
						Usage:       "Password for 2FA if exists",
						HideDefault: true,
					},
					&cli.BoolFlag{
						Name:        "new",
						Usage:       "Remove old session and create new one",
						HideDefault: true,
					},
				},
				Action: authCommand,
			},
		},
		Action: serve,
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal().Msg(err.Error())
	}
}
