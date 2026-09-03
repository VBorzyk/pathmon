package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/VBorzyk/pathmon/internal/config"
	"github.com/VBorzyk/pathmon/internal/detect"
)

const testToken = "123456:TEST-TOKEN"

// fakeAPI records every sendMessage request and answers with what the
// test tells it to. It stands in for api.telegram.org.
type fakeAPI struct {
	t        *testing.T
	server   *httptest.Server
	requests []sendMessageRequest
	paths    []string
	fail     bool // when true, answer like the Bot API does on an error
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{t: t}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req sendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		f.requests = append(f.requests, req)
		f.paths = append(f.paths, r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		if f.fail {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`))
			return
		}
		w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	t.Cleanup(f.server.Close)
	return f
}

func newTestTelegram(api *fakeAPI, cooldown time.Duration) *Telegram {
	return NewTelegram(config.Telegram{
		ChatID:   42,
		APIBase:  api.server.URL,
		Cooldown: cooldown,
	}, testToken, "observer-1")
}

var (
	blackhole = detect.Event{Type: detect.TypeBlackhole, Target: "1.1.1.1:443", Detail: "5 timeouts in a row"}
	slow      = detect.Event{Type: detect.TypeSlowConnect, Target: "my-vpn.example.com:443", Detail: "connect 340ms vs median 18ms"}
	recovered = detect.Event{Type: detect.TypeRecovered, Target: "1.1.1.1:443", Detail: "recovered after blackhole"}
)

func TestTelegramSendsOneDigestPerRound(t *testing.T) {
	api := newFakeAPI(t)
	tg := newTestTelegram(api, 15*time.Minute)
	at := time.Date(2026, 9, 4, 14, 22, 1, 0, time.UTC)

	if err := tg.Notify(at, []detect.Event{blackhole, slow}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(api.requests) != 1 {
		t.Fatalf("requests: got %d, want 1 (one digest per round)", len(api.requests))
	}
	req := api.requests[0]
	if req.ChatID != 42 {
		t.Errorf("chat_id: got %d, want 42", req.ChatID)
	}
	if want := "/bot" + testToken + "/sendMessage"; api.paths[0] != want {
		t.Errorf("path: got %q, want %q", api.paths[0], want)
	}
	for _, want := range []string{"observer-1", "14:22:01", "blackhole", "1.1.1.1:443", "slow_connect", "connect 340ms"} {
		if !strings.Contains(req.Text, want) {
			t.Errorf("message text is missing %q:\n%s", want, req.Text)
		}
	}
}

func TestTelegramSkipsEmptyRound(t *testing.T) {
	api := newFakeAPI(t)
	tg := newTestTelegram(api, 15*time.Minute)

	if err := tg.Notify(time.Now(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(api.requests) != 0 {
		t.Errorf("requests: got %d, want 0 for an empty round", len(api.requests))
	}
}

func TestTelegramCooldown(t *testing.T) {
	api := newFakeAPI(t)
	tg := newTestTelegram(api, 15*time.Minute)
	start := time.Date(2026, 9, 4, 14, 0, 0, 0, time.UTC)

	steps := []struct {
		name   string
		offset time.Duration
		events []detect.Event
		sent   bool
	}{
		{"first blackhole goes out", 0, []detect.Event{blackhole}, true},
		{"repeat inside cooldown is dropped", 5 * time.Minute, []detect.Event{blackhole}, false},
		{"recovered goes out", 6 * time.Minute, []detect.Event{recovered}, true},
		{"recovered again inside cooldown still goes out", 8 * time.Minute, []detect.Event{recovered}, true},
		{"different type on same target goes out", 7 * time.Minute, []detect.Event{slow}, true},
		{"repeat after cooldown goes out", 16 * time.Minute, []detect.Event{blackhole}, true},
	}

	for _, step := range steps {
		before := len(api.requests)
		if err := tg.Notify(start.Add(step.offset), step.events); err != nil {
			t.Fatalf("%s: unexpected error: %v", step.name, err)
		}
		if sent := len(api.requests) > before; sent != step.sent {
			t.Errorf("%s: sent=%v, want %v", step.name, sent, step.sent)
		}
	}
}

func TestTelegramRetriesUndeliveredDigest(t *testing.T) {
	api := newFakeAPI(t)
	tg := newTestTelegram(api, 0)
	at := time.Date(2026, 9, 4, 14, 22, 1, 0, time.UTC)

	api.fail = true
	err := tg.Notify(at, []detect.Event{blackhole})
	if err == nil {
		t.Fatalf("expected an error when the API answers ok=false")
	}
	if !strings.Contains(err.Error(), "chat not found") {
		t.Errorf("error should carry the API description, got: %v", err)
	}

	// Next round brings a new event; the failed digest must go out with it.
	api.fail = false
	if err := tg.Notify(at.Add(time.Minute), []detect.Event{slow}); err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}
	if len(api.requests) != 2 {
		t.Fatalf("requests: got %d, want 2 (failed + retry)", len(api.requests))
	}
	got := api.requests[1].Text
	if !strings.Contains(got, "blackhole") || !strings.Contains(got, "slow_connect") {
		t.Errorf("retry should carry both the pending and the new digest, got:\n%s", got)
	}
	if strings.Index(got, "blackhole") > strings.Index(got, "slow_connect") {
		t.Errorf("pending digest should come first (oldest first), got:\n%s", got)
	}

	// Once delivered, nothing is pending: an empty round sends nothing.
	if err := tg.Notify(at.Add(2*time.Minute), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(api.requests) != 2 {
		t.Errorf("requests: got %d, want 2 (pending must be cleared after success)", len(api.requests))
	}
}

func TestTelegramErrorDoesNotLeakToken(t *testing.T) {
	api := newFakeAPI(t)
	tg := newTestTelegram(api, 0)
	api.server.Close() // connection refused from now on

	err := tg.Notify(time.Now(), []detect.Event{blackhole})
	if err == nil {
		t.Fatalf("expected a transport error")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("error message leaks the bot token: %v", err)
	}
}
