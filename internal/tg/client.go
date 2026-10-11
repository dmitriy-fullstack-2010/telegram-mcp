package tg

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"
)

const (
	// defaultRateLimit is the number of Telegram API requests per second
	// allowed across all tool calls. Override with TG_RATE_LIMIT.
	defaultRateLimit = 1.0
	rateBurst        = 3

	// maxFloodWait is the longest FLOOD_WAIT the client sleeps through
	// (once) before giving up and reporting the error to the caller.
	maxFloodWait = 30 * time.Second
)

type Client struct {
	appID       int
	appHash     string
	sessionPath string

	limiter *rate.Limiter

	peersMu sync.Mutex
	peers   map[string]tg.InputPeerClass
	// linkedChats caches the discussion group name of broadcast channels
	// (empty string if the channel has none).
	linkedChats map[int64]string
}

func New(appID int, appHash, sessionPath string) *Client {
	limit := defaultRateLimit
	if v := os.Getenv("TG_RATE_LIMIT"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil && parsed > 0 {
			limit = parsed
		} else {
			log.Warn().Str("TG_RATE_LIMIT", v).Msg("invalid rate limit, using default")
		}
	}

	return &Client{
		appID:       appID,
		appHash:     appHash,
		sessionPath: sessionPath,
		limiter:     rate.NewLimiter(rate.Limit(limit), rateBurst),
		peers:       make(map[string]tg.InputPeerClass),
		linkedChats: make(map[int64]string),
	}
}

func (c *Client) T() *telegram.Client {
	opts := telegram.Options{
		SessionStorage: &telegram.FileSessionStorage{
			Path: c.sessionPath,
		},
		NoUpdates:   true,
		Middlewares: []telegram.Middleware{c.rateMiddleware()},
	}
	opts, _ = telegram.OptionsFromEnvironment(opts)
	return telegram.NewClient(c.appID, c.appHash, opts)
}

// rateMiddleware throttles every API request through the shared limiter and
// handles FLOOD_WAIT: short waits are slept through and retried once, longer
// ones are returned so the caller stops sending requests.
func (c *Client) rateMiddleware() telegram.Middleware {
	return telegram.MiddlewareFunc(func(next tg.Invoker) telegram.InvokeFunc {
		return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
			for attempt := 0; ; attempt++ {
				if err := c.limiter.Wait(ctx); err != nil {
					return err
				}

				err := next.Invoke(ctx, input, output)
				wait, ok := tgerr.AsFloodWait(err)
				if !ok {
					return err
				}

				if attempt > 0 || wait > maxFloodWait {
					return fmt.Errorf("telegram FLOOD_WAIT %s: too many requests, stop and retry later: %w", wait, err)
				}

				log.Warn().Dur("wait", wait).Msg("telegram FLOOD_WAIT, sleeping before retry")
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	})
}

// resolvePeer converts a dialog name into an input peer. Usernames are cached
// for the lifetime of the process because contacts.resolveUsername is one of
// the most strictly rate-limited Telegram methods.
func (c *Client) resolvePeer(ctx context.Context, api *tg.Client, name string) (tg.InputPeerClass, error) {
	isCustom := strings.Contains(name, "[") && strings.Contains(name, "]")

	switch {
	case strings.HasPrefix(name, "chn") && isCustom:
		var channelPeer tg.InputPeerChannel
		_, err := fmt.Sscanf(name, "chn[%d:%d]", &channelPeer.ChannelID, &channelPeer.AccessHash)
		if err != nil {
			return nil, errors.Wrapf(err, "scan channel peer(%q)", name)
		}

		return &channelPeer, nil
	case strings.HasPrefix(name, "cht") && isCustom:
		var chatPeer tg.InputPeerChat
		_, err := fmt.Sscanf(name, "cht[%d]", &chatPeer.ChatID)
		if err != nil {
			return nil, errors.Wrapf(err, "scan chat peer(%q)", name)
		}

		return &chatPeer, nil
	}

	name = usernameFromLink(name)
	key := strings.ToLower(strings.TrimPrefix(name, "@"))

	c.peersMu.Lock()
	cached, ok := c.peers[key]
	c.peersMu.Unlock()
	if ok {
		return cached, nil
	}

	sender := message.NewSender(api)
	inputPeer, err := sender.Resolve(name).AsInputPeer(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve name: %w", err)
	}

	c.peersMu.Lock()
	c.peers[key] = inputPeer
	c.peersMu.Unlock()

	return inputPeer, nil
}

// usernameFromLink turns public links like https://t.me/name/123 into "name".
// Private links (t.me/c/..., t.me/+...) and plain names are returned as is.
func usernameFromLink(name string) string {
	rest := strings.TrimPrefix(strings.TrimPrefix(name, "https://"), "http://")
	rest, ok := strings.CutPrefix(rest, "t.me/")
	if !ok {
		return name
	}

	rest = strings.TrimPrefix(rest, "s/")
	username, _, _ := strings.Cut(rest, "/")
	if username == "" || username == "c" || strings.HasPrefix(username, "+") || strings.HasPrefix(username, "joinchat") {
		return name
	}

	return username
}

// linkedChat returns the name of the discussion group linked to a broadcast
// channel, or "" if there is none. The result is cached per channel.
func (c *Client) linkedChat(ctx context.Context, api *tg.Client, channel *tg.Channel) (string, error) {
	if !channel.Broadcast {
		return "", nil
	}

	c.peersMu.Lock()
	cached, ok := c.linkedChats[channel.ID]
	c.peersMu.Unlock()
	if ok {
		return cached, nil
	}

	full, err := api.ChannelsGetFullChannel(ctx, channel.AsInput())
	if err != nil {
		return "", fmt.Errorf("get full channel: %w", err)
	}

	var name string
	if info, ok := full.FullChat.(*tg.ChannelFull); ok {
		if linkedID, ok := info.GetLinkedChatID(); ok {
			for _, chat := range full.Chats {
				if linked, ok := chat.(*tg.Channel); ok && linked.ID == linkedID {
					name = getUsername(linked)
				}
			}
		}
	}

	c.peersMu.Lock()
	c.linkedChats[channel.ID] = name
	c.peersMu.Unlock()

	return name, nil
}
