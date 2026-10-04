package mihomo

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/mingo-liu/vela/internal/profile"
)

func TestSubscriptionDownloadsUseConnectedProxy(t *testing.T) {
	for _, mode := range []string{"system", "tun"} {
		for _, operation := range []string{"import", "update", "replace", "automatic", "uncached selection"} {
			t.Run(mode+"/"+operation, func(t *testing.T) {
				const address = "http://subscription.invalid/config?token=private"
				const body = "proxy-groups:\n  - name: Downloaded\n    type: select\n    proxies: [DIRECT, REJECT]\n"
				requests := 0
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					requests++
					if request.URL.String() != address || request.UserAgent() != "Clash.Meta" {
						t.Errorf("unexpected proxy request: %s, %s", request.URL, request.UserAgent())
					}
					_, _ = w.Write([]byte(body))
				}))
				defer proxy.Close()
				proxyURL, _ := url.Parse(proxy.URL)
				port, _ := strconv.Atoi(proxyURL.Port())
				dir := t.TempDir()
				store := profile.NewStore(dir)
				urls := &testURLStore{}
				if operation != "import" {
					urls.value = `{"subscriptions":[{"id":"saved","url":"` + address + `","active":false}]}`
					if operation != "uncached selection" {
						if err := store.SaveSubscription("saved", "rules: ['MATCH,DIRECT']\n"); err != nil {
							t.Fatal(err)
						}
					}
				}
				subs := profile.NewSubscriptions(store, urls)
				runner := NewRunner(store, subs, dir, "", port, nil, nil)
				runner.state.Status = "running"
				runner.state.SystemProxyEnabled = mode == "system"
				runner.state.TunEnabled = mode == "tun"
				var err error
				switch operation {
				case "import":
					_, err = runner.ImportSubscription(address)
				case "update":
					_, err = runner.UpdateSubscription("saved")
				case "replace":
					_, err = runner.ReplaceSubscriptionURL("saved", address)
				case "automatic":
					runner.UpdateDueSubscriptions(time.Now(), time.Hour)
				case "uncached selection":
					_, err = runner.SelectSubscription("saved")
				}
				if err != nil {
					t.Fatal(err)
				}
				items, err := runner.Subscriptions()
				if err != nil || len(items) != 1 || requests != 1 || items[0].LastUpdateError != "" {
					t.Fatalf("proxy download: requests=%d, subscriptions=%+v, error=%v", requests, items, err)
				}
				cached, err := store.LoadSubscription(items[0].ID)
				if err != nil || string(cached) != body {
					t.Fatalf("downloaded config: %q, %v", cached, err)
				}
				state := runner.Snapshot()
				if state.Status != "running" || state.SystemProxyEnabled != (mode == "system") || state.TunEnabled != (mode == "tun") {
					t.Fatalf("download changed connection: %+v", state)
				}
				if operation != "uncached selection" && (items[0].Active || store.Exists()) {
					t.Fatal("download changed the selected configuration")
				}
			})
		}
	}
}
