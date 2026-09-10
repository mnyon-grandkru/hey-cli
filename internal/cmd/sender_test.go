package cmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/basecamp/hey-cli/internal/apierr"
)

func TestResolveSenderIDUnknownListsAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/identity.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":1,
			"senders":[
				{"id":111,"account_id":1,"email_address":"msnyon@hey.com","default":true},
				{"id":222,"account_id":1,"email_address":"mark@grandkru.com","contactable_type":"Person"},
				{"id":333,"account_id":1,"email_address":"me@mnyon.com","contactable_type":"Person"}
			]
		}`))
	}))
	t.Cleanup(server.Close)

	if err := runCLI(t, server, "senders", "list"); err != nil {
		t.Fatalf("senders list: %v", err)
	}

	// Unknown From fails with available list — and never posts a message.
	err := runCLI(t, server, "compose", "--from", "nobody@example.com",
		"--to", "alice@example.com", "--subject", "Nope", "-m", "body", "--draft")
	var cliErr *apierr.Error
	if !errors.As(err, &cliErr) || cliErr.Code != "usage" {
		t.Fatalf("unknown --from should be usage error, got %v", err)
	}
	if !strings.Contains(cliErr.Error(), "mark@grandkru.com") || !strings.Contains(cliErr.Error(), "me@mnyon.com") {
		t.Fatalf("usage error should list available senders, got %v", err)
	}
}

func TestComposeFromSetsActingSenderOnDraft(t *testing.T) {
	var gotSender float64
	var posted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/identity.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id":1,
				"senders":[
					{"id":111,"account_id":1,"email_address":"msnyon@hey.com","default":true},
					{"id":222,"account_id":1,"email_address":"mark@grandkru.com"}
				]
			}`))
		case r.Method == http.MethodPost && r.URL.Path == "/messages.json":
			posted = true
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			gotSender, _ = body["acting_sender_id"].(float64)
			entry, _ := body["entry"].(map[string]any)
			if status, _ := entry["status"].(string); status != "drafted" {
				t.Errorf("expected drafted status, got %#v", entry)
			}
			w.Header().Set("Location", "https://app.hey.com/messages/999")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	if err := runCLI(t, server, "compose",
		"--from", "Mark@GrandKru.com",
		"--to", "alice@example.com",
		"--subject", "Capital District",
		"-m", "Hello",
		"--draft"); err != nil {
		t.Fatalf("compose --from draft: %v", err)
	}
	if !posted {
		t.Fatal("expected draft POST /messages.json")
	}
	if gotSender != 222 {
		t.Fatalf("acting_sender_id = %v, want 222 for mark@grandkru.com", gotSender)
	}
}
