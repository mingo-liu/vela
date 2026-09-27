package mihomo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLookupExitIPInfoUsesLocalProxyAndParsesResponse(t *testing.T) {
	var requestedURL string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"success":true,"ip":"104.128.48.209","country":"United States","country_code":"US","region":"Illinois","city":"Waterloo","latitude":38.34,"longitude":-90.15,"connection":{"asn":30455,"isp":"Private Customer","org":"HostVenom LLC"},"timezone":{"id":"America/Chicago"}}`)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	info, err := lookupExitIPInfo(context.Background(), proxyURL, "http://example.invalid/ip")
	if err != nil {
		t.Fatal(err)
	}
	if requestedURL != "http://example.invalid/ip" {
		t.Fatalf("request bypassed local proxy: %q", requestedURL)
	}
	if info.IP != "104.128.48.209" || info.ASN != 30455 || info.ISP != "Private Customer" || info.Organization != "HostVenom LLC" || info.Timezone != "America/Chicago" || info.Latitude != 38.34 || info.Longitude != -90.15 {
		t.Fatalf("unexpected IP info: %+v", info)
	}
}

func TestLookupExitIPInfoRejectsInvalidResponse(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"success":false,"ip":"not-an-ip"}`)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = lookupExitIPInfo(context.Background(), proxyURL, "http://example.invalid/ip")
	if err == nil || !strings.Contains(err.Error(), "未返回有效数据") {
		t.Fatalf("expected invalid data error, got %v", err)
	}
}
