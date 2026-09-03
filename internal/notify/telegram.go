package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/VBorzyk/pathmon/internal/config"
	"github.com/VBorzyk/pathmon/internal/detect"
)

const (
	// sendTimeout bounds one delivery attempt. http.DefaultClient has no
	// timeout at all, and a hung request would stall the probe loop
	// exactly when the network is misbehaving.
	sendTimeout = 10 * time.Second

	// maxPending caps how many undelivered digests are kept for retry.
	// If Telegram is unreachable for hours, the oldest ones are dropped;
	// the journal is the place for a complete record, not the chat.
	maxPending = 20

	// maxTextLen is the Bot API limit for one message.
	maxTextLen = 4096
)

// Telegram sends event digests through the Bot API.
//
// One round produces at most one message. Repeats of the same event on
// the same target inside the cooldown are dropped, except "recovered",
// which always goes out: an alert without its closing line leaves the
// reader guessing.
//
// A digest that could not be sent is kept and retried on the next round
// together with anything new, so a short outage delays alerts instead
// of losing them.
type Telegram struct {
	cfg    config.Telegram
	token  string
	hostID string
	client *http.Client

	last    map[string]time.Time // target|type -> when it was last sent
	pending []string             // digests that failed to send, oldest first
}

// NewTelegram builds a notifier for the given chat. The token comes from
// the caller (read from the environment in main) and is never logged.
func NewTelegram(cfg config.Telegram, token, hostID string) *Telegram {
	return &Telegram{
		cfg:    cfg,
		token:  token,
		hostID: hostID,
		// Only the timeout is set. With Transport left nil the client uses
		// http.DefaultTransport, which honours HTTPS_PROXY / NO_PROXY from
		// the environment, so a proxy needs no code and no config field.
		client: &http.Client{Timeout: sendTimeout},
		last:   make(map[string]time.Time),
	}
}

// Notify applies the cooldown, formats the surviving events as one
// digest and sends it together with any digests still pending.
func (t *Telegram) Notify(at time.Time, events []detect.Event) error {
	if text := t.format(at, t.filter(at, events)); text != "" {
		t.pending = append(t.pending, text)
	}
	if len(t.pending) == 0 {
		return nil
	}

	if err := t.send(strings.Join(t.pending, "\n\n")); err != nil {
		if len(t.pending) > maxPending {
			t.pending = t.pending[len(t.pending)-maxPending:]
		}
		return err
	}
	t.pending = nil
	return nil
}

// filter drops events that were already reported within the cooldown.
// The round timestamp is the clock: it is what the reader sees, and it
// keeps the rule deterministic in tests.
func (t *Telegram) filter(at time.Time, events []detect.Event) []detect.Event {
	var kept []detect.Event
	for _, e := range events {
		key := e.Target + "|" + string(e.Type)
		if e.Type != detect.TypeRecovered && t.cfg.Cooldown > 0 {
			if last, ok := t.last[key]; ok && at.Sub(last) < t.cfg.Cooldown {
				continue
			}
		}
		t.last[key] = at
		kept = append(kept, e)
	}
	return kept
}

// format renders a batch as plain text. No parse_mode on purpose:
// Markdown would need escaping, and hostnames are full of underscores.
func (t *Telegram) format(at time.Time, events []detect.Event) string {
	if len(events) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "pathmon @ %s\n", t.hostID)
	for _, e := range events {
		fmt.Fprintf(&b, "%s  %s  %s\n  %s\n", at.Format("15:04:05"), e.Type, e.Target, e.Detail)
	}
	return strings.TrimRight(b.String(), "\n")
}

// sendMessageRequest is the JSON body of the Bot API sendMessage call.
type sendMessageRequest struct {
	ChatID                int64  `json:"chat_id"`
	Text                  string `json:"text"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

// apiResponse is the part of the Bot API reply we care about.
type apiResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

// send posts one message and reports failure if either the transport
// or the API said no.
func (t *Telegram) send(text string) error {
	if len(text) > maxTextLen {
		// Keep the tail: the newest events are the ones that matter.
		text = "[truncated]\n" + text[len(text)-maxTextLen+len("[truncated]\n"):]
	}

	body, err := json.Marshal(sendMessageRequest{
		ChatID:                t.cfg.ChatID,
		Text:                  text,
		DisableWebPagePreview: true,
	})
	if err != nil {
		return fmt.Errorf("telegram: encode request: %w", err)
	}

	endpoint := strings.TrimRight(t.cfg.APIBase, "/") + "/bot" + t.token + "/sendMessage"
	resp, err := t.client.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		// net/http wraps transport errors in *url.Error, which carries the
		// full URL, and the URL carries the token. Unwrap to the cause so
		// the token never reaches a log line.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return fmt.Errorf("telegram: %w", err)
	}
	defer resp.Body.Close()

	var reply apiResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&reply); err != nil {
		return fmt.Errorf("telegram: HTTP %d, unreadable reply: %w", resp.StatusCode, err)
	}
	if !reply.OK {
		return fmt.Errorf("telegram: HTTP %d: %s", resp.StatusCode, reply.Description)
	}
	return nil
}
