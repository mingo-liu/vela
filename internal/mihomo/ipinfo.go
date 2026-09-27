package mihomo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

const exitIPInfoURL = "https://ipwho.is/?fields=ip,success,message,country,country_code,region,city,latitude,longitude,connection,timezone"

// ExitIPInfo describes the public address observed through Vela's local proxy.
type ExitIPInfo struct {
	IP           string  `json:"ip"`
	Country      string  `json:"country"`
	CountryCode  string  `json:"countryCode"`
	Region       string  `json:"region"`
	City         string  `json:"city"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	ASN          int     `json:"asn"`
	ISP          string  `json:"isp"`
	Organization string  `json:"organization"`
	Timezone     string  `json:"timezone"`
}

func (r *Runner) ExitIPInfo() (ExitIPInfo, error) {
	state := r.Snapshot()
	if state.Status != "running" || !state.HasProfile || (!state.SystemProxyEnabled && !state.TunEnabled) {
		return ExitIPInfo{}, errors.New("请先连接代理以查询出口 IP")
	}
	proxyURL := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", fmt.Sprint(state.Port))}
	return lookupExitIPInfo(context.Background(), proxyURL, exitIPInfoURL)
}

func lookupExitIPInfo(ctx context.Context, proxyURL *url.URL, endpoint string) (ExitIPInfo, error) {
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 8 * time.Second, Transport: transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ExitIPInfo{}, err
	}
	response, err := client.Do(req)
	if err != nil {
		return ExitIPInfo{}, fmt.Errorf("查询出口 IP 失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ExitIPInfo{}, fmt.Errorf("IP 信息服务返回 HTTP %d", response.StatusCode)
	}
	var data struct {
		IP          string  `json:"ip"`
		Success     bool    `json:"success"`
		Country     string  `json:"country"`
		CountryCode string  `json:"country_code"`
		Region      string  `json:"region"`
		City        string  `json:"city"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		Connection  struct {
			ASN int    `json:"asn"`
			ISP string `json:"isp"`
			Org string `json:"org"`
		} `json:"connection"`
		Timezone struct {
			ID string `json:"id"`
		} `json:"timezone"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&data); err != nil {
		return ExitIPInfo{}, fmt.Errorf("解析 IP 信息失败: %w", err)
	}
	if !data.Success || net.ParseIP(data.IP) == nil {
		return ExitIPInfo{}, errors.New("IP 信息服务未返回有效数据")
	}
	return ExitIPInfo{IP: data.IP, Country: data.Country, CountryCode: data.CountryCode, Region: data.Region, City: data.City,
		Latitude: data.Latitude, Longitude: data.Longitude, ASN: data.Connection.ASN, ISP: data.Connection.ISP,
		Organization: data.Connection.Org, Timezone: data.Timezone.ID}, nil
}
