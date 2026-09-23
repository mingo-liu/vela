package profile

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	list, err := subs.List()
	if err != nil || len(list) != 1 || list[0].URL != address || !list[0].Active {
		t.Fatalf("subscription was not saved: %+v, %v", list, err)
	}
	body = "listeners: [{name: unsafe, type: mixed, port: 9999}]\n"
	if err := subs.Update(context.Background(), list[0].ID); err == nil {
		t.Fatal("invalid update was accepted")
	}
	stillSaved, err := subs.List()
	if err != nil || len(stillSaved) != 1 || stillSaved[0].UpdatedAt == nil || !stillSaved[0].UpdatedAt.Equal(*list[0].UpdatedAt) {
		t.Fatalf("failed update changed the subscription: %+v, %v", stillSaved, err)
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
	_, _, err := subs.fetch(context.Background(), "http://127.0.0.1:1/sub?token=private-token")
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
	if _, _, err := subs.fetch(context.Background(), server.URL); err == nil {
		t.Fatal("oversize response accepted")
	}
}

func TestSubscriptionCardsAndLegacyMigration(t *testing.T) {
	const body = "proxies: []\nrules:\n  - MATCH,DIRECT\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/one" {
			w.Header().Set("Subscription-Userinfo", "upload=10; download=20; total=100; expire=2000000000")
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	keychain := &memoryURLStore{value: server.URL + "/one"}
	subs := NewSubscriptions(NewStore(t.TempDir()), keychain)
	legacy, err := subs.List()
	if err != nil || len(legacy) != 1 || legacy[0].UpdatedAt != nil {
		t.Fatalf("legacy entry: %+v, %v", legacy, err)
	}
	if err := subs.Import(context.Background(), server.URL+"/one"); err != nil {
		t.Fatal(err)
	}
	if err := subs.Import(context.Background(), server.URL+"/two"); err != nil {
		t.Fatal(err)
	}
	if err := subs.Import(context.Background(), server.URL+"/two"); err != nil {
		t.Fatal(err)
	}
	list, err := subs.List()
	if err != nil || len(list) != 2 || list[0].Active || !list[1].Active {
		t.Fatalf("imported entries: %+v, %v", list, err)
	}
	if list[0].Upload == nil || *list[0].Upload != 10 || list[0].Download == nil || *list[0].Download != 20 || list[0].Total == nil || *list[0].Total != 100 || list[0].ExpiresAt == nil || list[0].ExpiresAt.Unix() != 2000000000 || list[0].UpdatedAt == nil {
		t.Fatalf("missing usage information: %+v", list[0])
	}
	if list[1].Total != nil || list[1].ExpiresAt != nil {
		t.Fatalf("missing headers should remain unknown: %+v", list[1])
	}
	if err := subs.Update(context.Background(), list[0].ID); err != nil {
		t.Fatal(err)
	}
	list, err = subs.List()
	if err != nil || !list[0].Active || list[1].Active || len(list) != 2 {
		t.Fatalf("updated entries: %+v, %v", list, err)
	}
	if err := subs.ImportLocal(body); err != nil {
		t.Fatal(err)
	}
	list, err = subs.List()
	if err != nil || list[0].Active || list[1].Active {
		t.Fatalf("local import should keep inactive subscriptions: %+v, %v", list, err)
	}
	if list[0].UpdatedAt.After(time.Now()) {
		t.Fatal("invalid update time")
	}
}

func TestSelectSubscriptionUsesCachedProfileOffline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := "One"
		if r.URL.Path == "/two" {
			name = "Two"
		}
		_, _ = w.Write([]byte("proxy-groups:\n  - name: " + name + "\n    type: select\n    proxies: [DIRECT, REJECT]\nrules: [MATCH,DIRECT]\n"))
	}))
	store := NewStore(t.TempDir())
	subs := NewSubscriptions(store, &memoryURLStore{})
	if err := subs.Import(context.Background(), server.URL+"/one"); err != nil {
		t.Fatal(err)
	}
	if err := subs.Import(context.Background(), server.URL+"/two"); err != nil {
		t.Fatal(err)
	}
	server.Close()
	list, err := subs.List()
	if err != nil {
		t.Fatal(err)
	}
	if err := subs.Select(context.Background(), list[0].ID); err != nil {
		t.Fatal(err)
	}
	groups, err := store.SelectorGroups()
	if err != nil || len(groups) != 2 || groups[0].Name != "One" || len(groups[0].Options) != 2 || groups[1].Name != "GLOBAL" {
		t.Fatalf("selected first subscription: %+v, %v", groups, err)
	}
	list, err = subs.List()
	if err != nil || !list[0].Active || list[1].Active {
		t.Fatalf("active subscription: %+v, %v", list, err)
	}
	if err := subs.Select(context.Background(), list[1].ID); err != nil {
		t.Fatal(err)
	}
	groups, err = store.SelectorGroups()
	if err != nil || len(groups) != 2 || groups[0].Name != "Two" {
		t.Fatalf("selected second subscription: %+v, %v", groups, err)
	}
}

func TestLegacyActiveSubscriptionIsCachedBeforeSwitch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("proxy-groups:\n  - name: New\n    type: select\n    proxies: [DIRECT]\n"))
	}))
	store := NewStore(t.TempDir())
	if err := store.Import("proxy-groups:\n  - name: Legacy\n    type: select\n    proxies: [REJECT]\n"); err != nil {
		t.Fatal(err)
	}
	keychain := &memoryURLStore{value: server.URL + "/legacy"}
	subs := NewSubscriptions(store, keychain)
	if err := subs.Import(context.Background(), server.URL+"/new"); err != nil {
		t.Fatal(err)
	}
	server.Close()
	if err := subs.Select(context.Background(), subscriptionID(server.URL+"/legacy")); err != nil {
		t.Fatal(err)
	}
	groups, err := store.SelectorGroups()
	if err != nil || len(groups) != 2 || groups[0].Name != "Legacy" {
		t.Fatalf("legacy profile was not restored: %+v, %v", groups, err)
	}
}
