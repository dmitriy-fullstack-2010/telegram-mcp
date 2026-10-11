package tg

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	mcp "github.com/metoro-io/mcp-golang"
	"github.com/pkg/errors"
)

const (
	maxContextIDs       = 10
	defaultParentsDepth = 3
	maxParentsDepth     = 5
	defaultRepliesLimit = 30
	maxRepliesLimit     = 100
	maxAround           = 20

	// getMessagesBatch is the maximum number of IDs per getMessages request.
	getMessagesBatch = 100
)

const (
	RoleHit    = "hit"
	RoleParent = "parent"
	RoleReply  = "reply"
	RoleAround = "around"
)

// nolint:lll
type ContextArguments struct {
	Name         string `json:"name" jsonschema:"required,description=Name of the dialog (same as in tg_search)"`
	IDs          []int  `json:"ids" jsonschema:"required,description=Message IDs to expand (from tg_search)\\, max 10 per call"`
	ParentsDepth int    `json:"parents_depth,omitempty" jsonschema:"description=How many levels of replied-to messages to load above each message (default 3\\, max 5)"`
	RepliesLimit int    `json:"replies_limit,omitempty" jsonschema:"description=Max replies to load per message (default 30\\, max 100). Only loaded for messages with replies > 0"`
	Around       int    `json:"around,omitempty" jsonschema:"description=Also load N messages before and N after each message to catch answers posted without reply (default 0\\, max 20). Costs one request per message"`
}

type ThreadMessage struct {
	MessageInfo
	Role string `json:"role"`
}

type Thread struct {
	HitID    int             `json:"hit_id"`
	Messages []ThreadMessage `json:"messages"`
}

type ContextResponse struct {
	Threads  []Thread `json:"threads"`
	NotFound []int    `json:"not_found,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// GetContext rebuilds the conversation around the given messages: the chain of
// messages they reply to, the replies to them and optionally the neighbouring
// messages. Requests are batched to keep the number of API calls low.
func (c *Client) GetContext(args ContextArguments) (*mcp.ToolResponse, error) {
	ids := uniqueIDs(args.IDs)
	if len(ids) == 0 {
		return nil, errors.New("ids must not be empty")
	}
	if len(ids) > maxContextIDs {
		return nil, errors.Errorf("too many ids: %d (max %d)", len(ids), maxContextIDs)
	}

	parentsDepth := args.ParentsDepth
	if parentsDepth <= 0 {
		parentsDepth = defaultParentsDepth
	}
	parentsDepth = min(parentsDepth, maxParentsDepth)

	repliesLimit := args.RepliesLimit
	if repliesLimit <= 0 {
		repliesLimit = defaultRepliesLimit
	}
	repliesLimit = min(repliesLimit, maxRepliesLimit)

	around := min(max(args.Around, 0), maxAround)

	h, _ := newHistory(nil)
	replies := make(map[int][]int) // hit id -> reply ids
	nearby := make(map[int][]int)  // hit id -> neighbouring ids
	var warnings []string

	client := c.T()
	if err := client.Run(context.Background(), func(ctx context.Context) (err error) {
		api := client.API()

		inputPeer, err := c.resolvePeer(ctx, api, args.Name)
		if err != nil {
			return fmt.Errorf("get inputPeer from name: %w", err)
		}

		// Hits and the chain of replied-to messages, one batch per level.
		frontier := ids
		for level := 0; level <= parentsDepth && len(frontier) > 0; level++ {
			if err := fetchMessages(ctx, api, inputPeer, frontier, h); err != nil {
				return fmt.Errorf("get messages: %w", err)
			}

			known := h.byID()
			var next []int
			for _, id := range frontier {
				m, ok := known[id]
				if !ok {
					continue
				}
				if parent := replyToID(m); parent != 0 {
					if _, ok := known[parent]; !ok {
						next = append(next, parent)
					}
				}
			}
			frontier = uniqueIDs(next)
		}

		known := h.byID()
		for _, id := range ids {
			m, ok := known[id]
			if !ok {
				continue
			}

			if r, ok := m.GetReplies(); ok && r.Replies > 0 {
				res, err := api.MessagesGetReplies(ctx, &tg.MessagesGetRepliesRequest{
					Peer:  inputPeer,
					MsgID: id,
					Limit: repliesLimit,
				})
				switch {
				case isFloodWait(err):
					return err
				case err != nil:
					// E.g. TOPIC_ID_INVALID in forum groups: keep the rest of the result.
					warnings = append(warnings, fmt.Sprintf("replies of %d: %v (use around instead)", id, err))
				default:
					replies[id] = messageIDs(res)
					if err := h.add(res); err != nil {
						return err
					}
				}
			}

			if around > 0 {
				var res tg.MessagesMessagesClass
				if _, topic := replyInfo(m); topic != 0 {
					// Neighbours within the same forum topic.
					res, err = api.MessagesGetReplies(ctx, &tg.MessagesGetRepliesRequest{
						Peer:      inputPeer,
						MsgID:     topic,
						OffsetID:  id,
						AddOffset: -around,
						Limit:     2*around + 1,
					})
				} else {
					res, err = api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
						Peer:      inputPeer,
						OffsetID:  id,
						AddOffset: -around,
						Limit:     2*around + 1,
					})
				}
				switch {
				case isFloodWait(err):
					return err
				case err != nil:
					warnings = append(warnings, fmt.Sprintf("messages around %d: %v", id, err))
				default:
					nearby[id] = messageIDs(res)
					if err := h.add(res); err != nil {
						return err
					}
				}
			}
		}

		return nil
	}); err != nil {
		return nil, errors.Wrap(err, "failed to get context")
	}

	known := h.byID()
	rsp := ContextResponse{Warnings: warnings}
	for _, id := range ids {
		hit, ok := known[id]
		if !ok {
			rsp.NotFound = append(rsp.NotFound, id)
			continue
		}

		// Later assignments win: around < reply < parent < hit.
		roles := make(map[int]string)
		for _, rid := range nearby[id] {
			roles[rid] = RoleAround
		}
		for _, rid := range replies[id] {
			roles[rid] = RoleReply
		}
		for parent, depth := replyToID(hit), 0; parent != 0 && depth < parentsDepth; depth++ {
			m, ok := known[parent]
			if !ok {
				break
			}
			roles[parent] = RoleParent
			parent = replyToID(m)
		}
		roles[id] = RoleHit

		thread := Thread{HitID: id}
		for mid, role := range roles {
			m, ok := known[mid]
			if !ok {
				continue
			}
			thread.Messages = append(thread.Messages, ThreadMessage{MessageInfo: h.messageInfo(m), Role: role})
		}
		sort.Slice(thread.Messages, func(i, j int) bool {
			return thread.Messages[i].ID < thread.Messages[j].ID
		})

		rsp.Threads = append(rsp.Threads, thread)
	}

	jsonData, err := json.Marshal(rsp)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal response")
	}

	return mcp.NewToolResponse(mcp.NewTextContent(string(jsonData))), nil
}

// fetchMessages loads messages by ID into h. Channels and supergroups need
// channels.getMessages; basic chats and users use messages.getMessages.
func fetchMessages(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, ids []int, h *history) error {
	for start := 0; start < len(ids); start += getMessagesBatch {
		batch := ids[start:min(start+getMessagesBatch, len(ids))]

		input := make([]tg.InputMessageClass, 0, len(batch))
		for _, id := range batch {
			input = append(input, &tg.InputMessageID{ID: id})
		}

		var (
			res tg.MessagesMessagesClass
			err error
		)
		if p, ok := peer.(*tg.InputPeerChannel); ok {
			res, err = api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
				Channel: &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
				ID:      input,
			})
		} else {
			res, err = api.MessagesGetMessages(ctx, input)
		}
		if err != nil {
			return err
		}

		if err := h.add(res); err != nil {
			return err
		}
	}

	return nil
}

func isFloodWait(err error) bool {
	_, ok := tgerr.AsFloodWait(err)
	return ok
}

func replyToID(m *tg.Message) int {
	replyTo, _ := replyInfo(m)
	return replyTo
}

func messageIDs(raw tg.MessagesMessagesClass) []int {
	h, err := newHistory(raw)
	if err != nil {
		return nil
	}

	ids := make([]int, 0, len(h.messages))
	for id := range h.byID() {
		ids = append(ids, id)
	}

	return ids
}

func uniqueIDs(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	res := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok || id <= 0 {
			continue
		}
		seen[id] = struct{}{}
		res = append(res, id)
	}

	return res
}
