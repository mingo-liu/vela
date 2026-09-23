package profile

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type memoryURLStore struct{ value string }

func (s *memoryURLStore) Get() (string, error) {
	if s.value == "" {
		return "", ErrNoSubscription
	}
	return s.value, nil
}
func (s *memoryURLStore) Put(value string) error { s.value = value; return nil }
func (s *memoryURLStore) Delete() error          { s.value = ""; return nil }

func TestSubscriptionImportAndUpdateKeepLastGoodProfile(t *testing.T) {
	body := "proxies: []\nrules:\n  - MATCH,DIRECT\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "private-token" {
			t.Error("missing subscription token")
		}
		if r.UserAgent() != "Clash.Meta" {
			_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte("trojan://example"))))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	store := NewStore(t.TempDir())
	keychain := &memoryURLStore{}
	subs := NewSubscriptions(store, keychain)
	address := server.URL + "/subscribe?token=private-token"
	if err := subs.Import(context.Background(), address); err != nil {
		t.Fatal(err)
	}
	if keychain.value != address {
		t.Fatal("subscription URL was not saved")
	}
	body = "listeners: [{name: unsafe, type: mixed, port: 9999}]\n"
	if err := subs.Update(context.Background()); err == nil {
		t.Fatal("invalid update was accepted")
	}
	got, err := store.Load()
	if err != nil || string(got) != "proxies: []\nrules:\n  - MATCH,DIRECT\n" {
		t.Fatalf("last good profile changed: %q, %v", got, err)
	}
}

func TestSubscriptionReportsBase64NodeList(t *testing.T) {
	data := []byte(base64.StdEncoding.EncodeToString([]byte("trojan://example\n")))
	if err := validateSubscription(data); err == nil || !strings.Contains(err.Error(), "Base64 节点列表") {
		t.Fatalf("unexpected format error: %v", err)
	}
}

func TestSubscriptionDownloadErrorDoesNotExposeToken(t *testing.T) {
	store := NewStore(t.TempDir())
	subs := NewSubscriptions(store, &memoryURLStore{})
	_, err := subs.fetch(context.Background(), "http://127.0.0.1:1/sub?token=private-token")
	if err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("unsafe download error: %v", err)
	}
	if _, err := (&memoryURLStore{}).Get(); !errors.Is(err, ErrNoSubscription) {
		t.Fatal(err)
	}
}

func TestSubscriptionRejectsLargeResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", MaxConfigSize+1)))
	}))
	defer server.Close()
	subs := NewSubscriptions(NewStore(t.TempDir()), &memoryURLStore{})
	if _, err := subs.fetch(context.Background(), server.URL); err == nil {
		t.Fatal("oversize response accepted")
	}
}
