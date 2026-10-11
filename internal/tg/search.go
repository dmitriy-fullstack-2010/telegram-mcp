package tg

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gotd/td/tg"
	mcp "github.com/metoro-io/mcp-golang"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

const (
	defaultSearchLimit = 30
	maxSearchLimit     = 100
)

// nolint:lll
type SearchArguments struct {
	Name     string `json:"name" jsonschema:"required,description=Name of the dialog: username (e.g. diasporaUAinSpain)\\, t.me link or name from tg_dialogs"`
	Query    string `json:"query" jsonschema:"required,description=Search text. Telegram matches whole words without morphology\\, so use short keywords"`
	Limit    int    `json:"limit,omitempty" jsonschema:"description=Max messages to return (default 30\\, max 100)"`
	OffsetID int    `json:"offset_id,omitempty" jsonschema:"description=Continuation: offset_id from the previous response"`
	MinDate  string `json:"min_date,omitempty" jsonschema:"description=Only messages on or after this date (YYYY-MM-DD)"`
	MaxDate  string `json:"max_date,omitempty" jsonschema:"description=Only messages on or before this date (YYYY-MM-DD)"`
	From     string `json:"from,omitempty" jsonschema:"description=Only messages from this username"`
	Topic    int    `json:"topic,omitempty" jsonschema:"description=Forum groups only: search inside this topic (id from tg_topics). Omit to search the whole group"`
}

type SearchResponse struct {
	Messages []MessageInfo `json:"messages"`
	Total    int           `json:"total"`
	OffsetID int           `json:"offset_id,omitempty"`
	// LinkedChat is the discussion group of a broadcast channel, where
	// comments and conversations usually are. Search it with tg_search too.
	LinkedChat string `json:"linked_chat,omitempty"`
}

// Search finds messages in a dialog with a single messages.search request.
func (c *Client) Search(args SearchArguments) (*mcp.ToolResponse, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	limit = min(limit, maxSearchLimit)

	minDate, err := parseDate(args.MinDate, false)
	if err != nil {
		return nil, errors.Wrap(err, "invalid min_date")
	}

	maxDate, err := parseDate(args.MaxDate, true)
	if err != nil {
		return nil, errors.Wrap(err, "invalid max_date")
	}

	var (
		h          *history
		linkedChat string
	)
	client := c.T()
	if err := client.Run(context.Background(), func(ctx context.Context) (err error) {
		api := client.API()

		inputPeer, err := c.resolvePeer(ctx, api, args.Name)
		if err != nil {
			return fmt.Errorf("get inputPeer from name: %w", err)
		}

		req := &tg.MessagesSearchRequest{
			Peer:     inputPeer,
			Q:        args.Query,
			Filter:   &tg.InputMessagesFilterEmpty{},
			MinDate:  minDate,
			MaxDate:  maxDate,
			OffsetID: args.OffsetID,
			Limit:    limit,
		}

		if args.From != "" {
			fromPeer, err := c.resolvePeer(ctx, api, args.From)
			if err != nil {
				return fmt.Errorf("resolve from(%q): %w", args.From, err)
			}
			req.SetFromID(fromPeer)
		}

		if args.Topic != 0 {
			req.SetTopMsgID(args.Topic)
		}

		messagesClass, err := api.MessagesSearch(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to search: %w", err)
		}

		h, err = newHistory(messagesClass)
		if err != nil {
			return fmt.Errorf("failed to process search results: %w", err)
		}

		// Discussions of a broadcast channel live in its linked group:
		// report it so the caller can search there too.
		if p, ok := inputPeer.(*tg.InputPeerChannel); ok {
			if channel, ok := h.channels[p.ChannelID]; ok {
				linkedChat, err = c.linkedChat(ctx, api, channel)
				if err != nil {
					log.Warn().Err(err).Msg("failed to get linked chat")
				}
			}
		}

		return nil
	}); err != nil {
		return nil, errors.Wrap(err, "failed to search")
	}

	rsp := SearchResponse{
		Messages:   h.Info(),
		Total:      h.count,
		LinkedChat: linkedChat,
	}
	if len(h.messages) >= limit {
		rsp.OffsetID = h.Offset()
	}

	jsonData, err := json.Marshal(rsp)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal response")
	}

	return mcp.NewToolResponse(mcp.NewTextContent(string(jsonData))), nil
}

// parseDate converts YYYY-MM-DD into a unix timestamp in local time. With
// endOfDay the last second of that day is returned.
func parseDate(value string, endOfDay bool) (int, error) {
	if value == "" {
		return 0, nil
	}

	t, err := time.ParseInLocation(time.DateOnly, value, time.Local)
	if err != nil {
		return 0, err
	}

	if endOfDay {
		t = t.AddDate(0, 0, 1).Add(-time.Second)
	}

	return int(t.Unix()), nil
}
