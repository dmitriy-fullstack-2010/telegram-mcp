package tg

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gotd/td/tg"
	mcp "github.com/metoro-io/mcp-golang"
	"github.com/pkg/errors"
)

type HistoryArguments struct {
	Name   string `json:"name" jsonschema:"required,description=Name of the dialog"`
	Offset int    `json:"offset,omitempty" jsonschema:"description=Offset for continuation"`
}

type HistoryResponse struct {
	Messages []MessageInfo `json:"messages"`
	Offset   int           `json:"offset,omitempty"`
}

func (c *Client) GetHistory(args HistoryArguments) (*mcp.ToolResponse, error) {
	var messagesClass tg.MessagesMessagesClass
	client := c.T()
	if err := client.Run(context.Background(), func(ctx context.Context) (err error) {
		api := client.API()

		inputPeer, err := c.resolvePeer(ctx, api, args.Name)
		if err != nil {
			return fmt.Errorf("get inputPeer from name: %w", err)
		}

		messagesClass, err = api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
			Peer:     inputPeer,
			OffsetID: args.Offset,
		})
		if err != nil {
			return fmt.Errorf("failed to get history: %w", err)
		}

		//Debug
		//jsonData, _ := json.Marshal(messagesClass)
		//log.Info().RawJSON("history", cleanJSON(jsonData)).Msg("history")

		return nil
	}); err != nil {
		return nil, errors.Wrap(err, "failed to get history")
	}

	h, err := newHistory(messagesClass)
	if err != nil {
		return nil, errors.Wrap(err, "failed to process history")
	}

	rsp := HistoryResponse{
		Messages: h.Info(),
		Offset:   h.Offset(),
	}

	jsonData, err := json.Marshal(rsp)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal response")
	}

	return mcp.NewToolResponse(mcp.NewTextContent(string(jsonData))), nil
}

// history accumulates messages together with the users and chats needed to
// render them. Several API responses can be merged into one history.
type history struct {
	messages []tg.MessageClass
	count    int

	users    map[int64]*tg.User
	chats    map[int64]*tg.Chat
	channels map[int64]*tg.Channel
}

func newHistory(raw tg.MessagesMessagesClass) (*history, error) {
	h := &history{
		users:    make(map[int64]*tg.User),
		chats:    make(map[int64]*tg.Chat),
		channels: make(map[int64]*tg.Channel),
	}

	if raw == nil {
		return h, nil
	}

	if err := h.add(raw); err != nil {
		return nil, err
	}

	return h, nil
}

// add merges an API response into the history. The total count is taken from
// the latest response.
func (h *history) add(raw tg.MessagesMessagesClass) error {
	var (
		messages []tg.MessageClass
		users    []tg.UserClass
		chats    []tg.ChatClass
	)

	switch m := raw.(type) {
	case *tg.MessagesMessages:
		messages, users, chats = m.Messages, m.Users, m.Chats
		h.count = len(m.Messages)
	case *tg.MessagesMessagesSlice:
		messages, users, chats = m.Messages, m.Users, m.Chats
		h.count = m.Count
	case *tg.MessagesChannelMessages:
		messages, users, chats = m.Messages, m.Users, m.Chats
		h.count = m.Count
	default:
		return fmt.Errorf("unexpected type: %T", raw)
	}

	h.messages = append(h.messages, messages...)

	for _, u := range users {
		if user, ok := u.(*tg.User); ok {
			h.users[user.ID] = user
		}
	}

	for _, c := range chats {
		switch chat := c.(type) {
		case *tg.Chat:
			h.chats[chat.ID] = chat
		case *tg.Channel:
			h.channels[chat.ID] = chat
		}
	}

	return nil
}

func (h *history) Info() []MessageInfo {
	messages := make([]MessageInfo, 0, len(h.messages))

	for _, msg := range h.messages {
		m, ok := msg.(*tg.Message)
		if !ok {
			continue
		}

		messages = append(messages, h.messageInfo(m))
	}

	return messages
}

func (h *history) messageInfo(m *tg.Message) MessageInfo {
	info := MessageInfo{
		ID:   m.ID,
		Who:  h.author(m),
		When: time.Unix(int64(m.Date), 0).Format(time.DateTime),
		Text: m.Message,
		Link: h.link(m),
		ts:   m.Date,
	}

	info.ReplyTo, info.Topic = replyInfo(m)

	if replies, ok := m.GetReplies(); ok {
		info.Replies = replies.Replies
	}

	return info
}

// replyInfo returns the ID of the message m replies to and, in forum groups,
// the ID of its topic. A message posted directly into a topic carries the
// topic root as ReplyToMsgID, which is not a real reply. Messages of the
// General topic have no topic ID.
func replyInfo(m *tg.Message) (replyTo, topic int) {
	header, ok := m.ReplyTo.(*tg.MessageReplyHeader)
	if !ok {
		return 0, 0
	}

	if !header.ForumTopic {
		return header.ReplyToMsgID, 0
	}

	if top, ok := header.GetReplyToTopID(); ok {
		return header.ReplyToMsgID, top
	}

	return 0, header.ReplyToMsgID
}

// author returns "First Last (@username)" for users, or the chat title for
// messages sent on behalf of a channel or group.
func (h *history) author(m *tg.Message) string {
	from := m.FromID
	if from == nil {
		// Channel posts have no sender; they are authored by the channel.
		from = m.PeerID
	}

	switch p := from.(type) {
	case *tg.PeerUser:
		user, ok := h.users[p.UserID]
		if !ok {
			return ""
		}

		name := getTitle(user)
		if user.Username != "" {
			if name == "" {
				return "@" + user.Username
			}
			name += " (@" + user.Username + ")"
		}

		return name
	case *tg.PeerChannel:
		if channel, ok := h.channels[p.ChannelID]; ok {
			return getTitle(channel)
		}
	case *tg.PeerChat:
		if chat, ok := h.chats[p.ChatID]; ok {
			return getTitle(chat)
		}
	}

	return ""
}

// link returns a t.me link to the message. Only supergroups and channels have
// message links; for private ones the link works for members only.
func (h *history) link(m *tg.Message) string {
	p, ok := m.PeerID.(*tg.PeerChannel)
	if !ok {
		return ""
	}

	if channel, ok := h.channels[p.ChannelID]; ok && channel.Username != "" {
		return fmt.Sprintf("https://t.me/%s/%d", channel.Username, m.ID)
	}

	return fmt.Sprintf("https://t.me/c/%d/%d", p.ChannelID, m.ID)
}

// byID returns all regular messages of the history keyed by message ID.
func (h *history) byID() map[int]*tg.Message {
	res := make(map[int]*tg.Message, len(h.messages))
	for _, msg := range h.messages {
		if m, ok := msg.(*tg.Message); ok {
			res[m.ID] = m
		}
	}

	return res
}

func (h *history) Offset() int {
	for i := len(h.messages) - 1; i >= 0; i-- {
		if msg, ok := h.messages[i].(*tg.Message); ok {
			return msg.ID
		}
	}

	return 0
}
