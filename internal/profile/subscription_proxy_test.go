package profile

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSubscriptionHTTPSDownloadThroughProxy(t *testing.T) {
	const body = "rules: ['MATCH,DIRECT']\n"
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/redirect" {
			http.Redirect(w, request, "/config", http.StatusFound)
			return
		}
		if request.URL.Path == "/downgrade" {
			http.Redirect(w, request, "http://subscription.invalid/config", http.StatusFound)
			return
		}
		if request.UserAgent() != "Clash.Meta" {
			t.Errorf("unexpected user agent: %s", request.UserAgent())
		}
		w.Header().Set("Subscription-Userinfo", "upload=10; download=20; total=100")
		_, _ = w.Write([]byte(body))
	}))
	defer origin.Close()
	var tunnels atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodConnect || request.Host != "subscription.invalid:443" {
			t.Errorf("unexpected tunnel request: %s %s", request.Method, request.Host)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		tunnels.Add(1)
		upstream, err := net.Dial("tcp", origin.Listener.Addr().String())
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer upstream.Close()
		downstream, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer downstream.Close()
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		if err := buffered.Flush(); err != nil {
			t.Error(err)
			return
		}
		done := make(chan struct{})
		go func() {
			_, _ = io.Copy(upstream, buffered)
			_ = upstream.Close()
			close(done)
		}()
		_, _ = io.Copy(downstream, upstream)
		_ = downstream.Close()
		<-done
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	subs := NewSubscriptions(NewStore(t.TempDir()), &memoryURLStore{})
	// Trust only the test origin certificate and retain TLS verification in the tunnel.
	transport := subs.client.Transport.(*http.Transport)
	transport.TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	transport.TLSClientConfig.ServerName = "127.0.0.1"
	download, err := subs.DownloadWithProxy(context.Background(), "https://subscription.invalid/redirect", proxyURL)
	if err != nil || string(download.Data) != body || download.Info.Download == nil || *download.Info.Download != 20 || tunnels.Load() == 0 {
		t.Fatalf("HTTPS proxy download: %+v, tunnels=%d, error=%v", download, tunnels.Load(), err)
	}
	if _, err := subs.DownloadWithProxy(context.Background(), "https://subscription.invalid/downgrade", proxyURL); err == nil {
		t.Fatal("proxy download accepted a protocol-changing redirect")
	}
	if transport.Proxy != nil {
		t.Fatal("proxy download changed the disconnected client")
	}
	if _, err := subs.Download(context.Background(), origin.URL+"/config"); err != nil {
		t.Fatalf("direct download after proxy download: %v", err)
	}
}

func TestSubscriptionProxyDownloadValidatesContentAndRedactsErrors(t *testing.T) {
	for _, body := range []string{"listeners: [{name: unsafe, type: mixed, port: 9999}]\n", strings.Repeat("x", MaxConfigSize+1)} {
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		proxyURL, _ := url.Parse(proxy.URL)
		subs := NewSubscriptions(NewStore(t.TempDir()), &memoryURLStore{})
		if _, err := subs.DownloadWithProxy(context.Background(), "http://subscription.invalid/config?token=private", proxyURL); err == nil || strings.Contains(err.Error(), "private") {
			t.Errorf("unexpected validation error: %v", err)
		}
		proxy.Close()
		if _, err := subs.DownloadWithProxy(context.Background(), "http://subscription.invalid/config?token=private", proxyURL); err == nil || strings.Contains(err.Error(), "private") {
			t.Errorf("unsafe proxy connection error: %v", err)
		}
	}
}
