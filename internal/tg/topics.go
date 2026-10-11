package tg

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gotd/td/tg"
	mcp "github.com/metoro-io/mcp-golang"
	"github.com/pkg/errors"
)

const maxTopicsLimit = 100

// nolint:lll
type TopicsArguments struct {
	Name  string `json:"name" jsonschema:"required,description=Name of the forum group (username or t.me link)"`
	Query string `json:"query,omitempty" jsonschema:"description=Only topics whose title contains this text"`
}

type TopicInfo struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Closed bool   `json:"closed,omitempty"`
}

type TopicsResponse struct {
	Topics []TopicInfo `json:"topics"`
	Total  int         `json:"total"`
}

// GetTopics lists topics of a forum group with one channels.getForumTopics
// request (up to 100 topics).
func (c *Client) GetTopics(args TopicsArguments) (*mcp.ToolResponse, error) {
	var res *tg.MessagesForumTopics
	client := c.T()
	if err := client.Run(context.Background(), func(ctx context.Context) (err error) {
		api := client.API()

		inputPeer, err := c.resolvePeer(ctx, api, args.Name)
		if err != nil {
			return fmt.Errorf("get inputPeer from name: %w", err)
		}

		p, ok := inputPeer.(*tg.InputPeerChannel)
		if !ok {
			return errors.New("topics are only available in forum supergroups")
		}

		req := &tg.ChannelsGetForumTopicsRequest{
			Channel: &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
			Limit:   maxTopicsLimit,
		}
		if args.Query != "" {
			req.SetQ(args.Query)
		}

		res, err = api.ChannelsGetForumTopics(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to get topics: %w", err)
		}

		return nil
	}); err != nil {
		return nil, errors.Wrap(err, "failed to get topics")
	}

	rsp := TopicsResponse{Total: res.Count}
	for _, t := range res.Topics {
		if topic, ok := t.(*tg.ForumTopic); ok {
			rsp.Topics = append(rsp.Topics, TopicInfo{ID: topic.ID, Title: topic.Title, Closed: topic.Closed})
		}
	}

	jsonData, err := json.Marshal(rsp)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal response")
	}

	return mcp.NewToolResponse(mcp.NewTextContent(string(jsonData))), nil
}
