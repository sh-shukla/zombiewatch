package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh-shukla/zombiewatch/internal/scan"
)

func payload() Payload {
	return Payload{
		Source: "zombiewatch", Watch: "default", ScanTime: time.Now(),
		Zombies: []scan.Zombie{{Check: "dead-nodes", Kind: "Node", Object: "node-1", Detail: "NotReady for 2h"}},
	}
}

func TestSendGeneric(t *testing.T) {
	var got Payload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()

	if err := Send(context.Background(), srv.URL, "generic", payload()); err != nil {
		t.Fatal(err)
	}
	if len(got.Zombies) != 1 || got.Zombies[0].Object != "node-1" {
		t.Fatalf("payload did not round-trip: %+v", got)
	}
}

func TestSendSlackFormat(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()

	if err := Send(context.Background(), srv.URL, "slack", payload()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got["text"], "node-1") || !strings.Contains(got["text"], "zombiewatch") {
		t.Fatalf("slack text missing content: %q", got["text"])
	}
}

func TestSendFailsLoudOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	err := Send(context.Background(), srv.URL, "generic", payload())
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("want loud 403 error, got %v", err)
	}
}
