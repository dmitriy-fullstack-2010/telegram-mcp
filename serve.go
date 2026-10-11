package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/dmitriy-fullstack-2010/telegram-mcp/internal/tg"
	"github.com/invopop/jsonschema"

	mcp "github.com/metoro-io/mcp-golang"
	"github.com/metoro-io/mcp-golang/transport/stdio"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

const searchDescription = "Search messages in a telegram dialog (group, channel, chat) by text. " +
	"One request per call. Telegram matches whole words without morphology, so run several short " +
	"keyword variants (e.g. in Ukrainian and Russian: 'паспорт', 'закордонний', 'загранпаспорт') " +
	"and use min_date to focus on recent information. Each result has id, reply_to (id of the message " +
	"it answers) and replies (number of replies). Then call tg_context only for the most relevant ids. " +
	"If the response has linked_chat, the dialog is a channel and its discussions are in that group: search it too."

const contextDescription = "Rebuild the conversation around messages found by tg_search: the chain of " +
	"messages they reply to (parents), replies to them and optionally neighbouring messages (around). " +
	"Up to 10 ids per call, batched to keep Telegram requests low; prefer one call with several ids over " +
	"many calls. Use around (e.g. 5-10) when answers may have been posted without using reply."

const topicsDescription = "List topics of a forum group (a group split into topics). One request. " +
	"Messages in search results have topic (topic id); pass topic to tg_search to search inside one topic. " +
	"Messages of the General topic have no topic id."

func serve(ctx context.Context, cmd *cli.Command) error {
	appID := cmd.Int("app-id")
	appHash := cmd.String("api-hash")
	sessionPath := cmd.String("session")
	dryRun := cmd.Bool("dry")

	if schemaURL := cmd.String("schema-version"); schemaURL != "" {
		jsonschema.Version = schemaURL
	}

	_, err := os.Stat(sessionPath)
	if err != nil {
		return fmt.Errorf("session file not found(%s): %w", sessionPath, err)
	}

	server := mcp.NewServer(stdio.NewStdioServerTransport())
	client := tg.New(int(appID), appHash, sessionPath)

	if dryRun {
		answer, err := client.GetMe(tg.EmptyArguments{})
		if err != nil {
			return fmt.Errorf("get user: %w", err)
		}

		data, err := json.MarshalIndent(answer, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}

		log.Info().RawJSON("answer", data).Msg("Check GetMe: OK")

		answer, err = client.GetDialogs(tg.DialogsArguments{Offset: "", OnlyUnread: true})
		if err != nil {
			return fmt.Errorf("get dialogs: %w", err)
		}

		log.Info().RawJSON("answer", []byte(answer.Content[0].TextContent.Text)).Msg("Check GetDialogs: OK")

		answer, err = client.GetHistory(tg.HistoryArguments{Name: os.Getenv("TG_TEST_USERNAME")})
		if err != nil {
			return fmt.Errorf("get nickname history: %w", err)
		}

		answer, err = client.GetHistory(tg.HistoryArguments{Name: "cht[4626931529]"})
		if err != nil {
			return fmt.Errorf("get chat history: %w", err)
		}

		answer, err = client.GetHistory(tg.HistoryArguments{Name: "chn[2225853048:8934705438195741763]"})
		if err != nil {
			return fmt.Errorf("get chan history: %w", err)
		}

		log.Info().RawJSON("answer", []byte(answer.Content[0].TextContent.Text)).Msg("Check GetHistory: OK")

		if chat := os.Getenv("TG_TEST_SEARCH_CHAT"); chat != "" {
			answer, err = client.Search(tg.SearchArguments{Name: chat, Query: os.Getenv("TG_TEST_SEARCH_QUERY"), Limit: 5})
			if err != nil {
				return fmt.Errorf("search: %w", err)
			}

			log.Info().RawJSON("answer", []byte(answer.Content[0].TextContent.Text)).Msg("Check Search: OK")

			var found tg.SearchResponse
			if err := json.Unmarshal([]byte(answer.Content[0].TextContent.Text), &found); err == nil && len(found.Messages) > 0 {
				answer, err = client.GetContext(tg.ContextArguments{Name: chat, IDs: []int{found.Messages[0].ID}, Around: 3})
				if err != nil {
					return fmt.Errorf("context: %w", err)
				}

				log.Info().RawJSON("answer", []byte(answer.Content[0].TextContent.Text)).Msg("Check Context: OK")
			}
		}

		answer, err = client.SendDraft(tg.DraftArguments{Name: os.Getenv("TG_TEST_USERNAME"), Text: "test draft"})
		if err != nil {
			log.Err(err).Msg("Check SendDraft: FAIL")
		} else {
			log.Info().RawJSON("answer", []byte(answer.Content[0].TextContent.Text)).Msg("Check SendDraft: OK")
		}

		answer, err = client.ReadHistory(tg.ReadArguments{Name: os.Getenv("TG_TEST_USERNAME")})
		if err != nil {
			log.Err(err).Msg("Check ReadHistory: FAIL")
		} else {
			log.Info().RawJSON("answer", []byte(answer.Content[0].TextContent.Text)).Msg("Check ReadHistory: OK")
		}

		return nil
	}

	err = server.RegisterTool("tg_me", "Get current telegram account info", client.GetMe)
	if err != nil {
		return fmt.Errorf("register tool: %w", err)
	}

	err = server.RegisterTool("tg_dialogs", "Get list of telegram dialogs (chats, channels, users)", client.GetDialogs)
	if err != nil {
		return fmt.Errorf("register dialogs tool: %w", err)
	}

	err = server.RegisterTool("tg_dialog", "Get messages of telegram dialog", client.GetHistory)
	if err != nil {
		return fmt.Errorf("register dialogs tool: %w", err)
	}

	err = server.RegisterTool("tg_search", searchDescription, client.Search)
	if err != nil {
		return fmt.Errorf("register search tool: %w", err)
	}

	err = server.RegisterTool("tg_context", contextDescription, client.GetContext)
	if err != nil {
		return fmt.Errorf("register context tool: %w", err)
	}

	err = server.RegisterTool("tg_topics", topicsDescription, client.GetTopics)
	if err != nil {
		return fmt.Errorf("register topics tool: %w", err)
	}

	err = server.RegisterTool("tg_send", "Send draft message to dialog", client.SendDraft)
	if err != nil {
		return fmt.Errorf("register dialogs tool: %w", err)
	}

	err = server.RegisterTool("tg_read", "Mark dialog messages as read", client.ReadHistory)
	if err != nil {
		return fmt.Errorf("register read tool: %w", err)
	}

	if err := server.Serve(); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	<-ctx.Done()

	return nil
}
