package tg

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gotd/td/tg"
	mcp "github.com/metoro-io/mcp-golang"
	"github.com/pkg/errors"
)

type ReadArguments struct {
	Name string `json:"name" jsonschema:"description=Name of the dialog"`
}

type ReadResponse struct {
	Result string `json:"result"`
}

func (c *Client) ReadHistory(args ReadArguments) (*mcp.ToolResponse, error) {
	ctx := context.Background()

	var affectedMsgs *tg.MessagesAffectedMessages
	client := c.T()
	if err := client.Run(ctx, func(ctx context.Context) error {
		api := client.API()

		inputPeer, err := c.resolvePeer(ctx, api, args.Name)
		if err != nil {
			return fmt.Errorf("get inputPeer from name: %w", err)
		}

		switch p := inputPeer.(type) {
		case *tg.InputPeerUser, *tg.InputPeerChat:
			affectedMsgs, err = api.MessagesReadHistory(ctx, &tg.MessagesReadHistoryRequest{
				Peer: inputPeer,
			})

		case *tg.InputPeerChannel:
			var ok bool
			ok, err = api.ChannelsReadHistory(ctx, &tg.ChannelsReadHistoryRequest{
				Channel: &tg.InputChannel{
					ChannelID:  p.ChannelID,
					AccessHash: p.AccessHash,
				},
			})
			if err != nil {
				return fmt.Errorf("failed to read channels: %w", err)
			}

			affectedMsgs = &tg.MessagesAffectedMessages{}
			if ok {
				affectedMsgs = &tg.MessagesAffectedMessages{
					Pts:      1,
					PtsCount: 1,
				}
			}
		default:
			return fmt.Errorf("unexpected input peer type: %T", p)
		}

		if err != nil {
			return fmt.Errorf("read history: %w", err)
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("run client: %w", err)
	}

	res := "done"
	if affectedMsgs.PtsCount == 0 {
		res = "unread messages not found"
	}

	jsonData, err := json.Marshal(ReadResponse{Result: res})
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal response")
	}

	return mcp.NewToolResponse(mcp.NewTextContent(string(jsonData))), nil
}
