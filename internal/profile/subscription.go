package profile

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type URLStore interface {
	Get() (string, error)
	Put(string) error
	Delete() error
}

type Subscription struct {
	ID        string     `json:"id"`
	URL       string     `json:"url"`
	Active    bool       `json:"active"`
	UpdatedAt *time.Time `json:"updatedAt"`
	ExpiresAt *time.Time `json:"expiresAt"`
	Upload    *int64     `json:"upload"`
	Download  *int64     `json:"download"`
	Total     *int64     `json:"total"`
}

type subscriptionCatalog struct {
	Subscriptions []Subscription `json:"subscriptions"`
}

type Subscriptions struct {
	profiles *Store
	urls     URLStore
	client   *http.Client
}

func NewSubscriptions(profiles *Store, urls URLStore) *Subscriptions {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}
	client := &http.Client{Timeout: 20 * time.Second, Transport: transport}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 3 || request.URL.Scheme != via[0].URL.Scheme {
			return errors.New("订阅重定向超出限制或改变协议")
		}
		return nil
	}
	return &Subscriptions{profiles: profiles, urls: urls, client: client}
}

func subscriptionID(address string) string {
	sum := sha256.Sum256([]byte(address))
	return hex.EncodeToString(sum[:])
}

func newSubscriptionID(catalog subscriptionCatalog) (string, error) {
	for {
		var bytes [16]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return "", err
		}
		id := hex.EncodeToString(bytes[:])
		found := false
		for _, subscription := range catalog.Subscriptions {
			if subscription.ID == id {
				found = true
				break
			}
		}
		if !found {
			return id, nil
		}
	}
}

func (s *Subscriptions) catalog() (subscriptionCatalog, string, error) {
	raw, err := s.urls.Get()
	if errors.Is(err, ErrNoSubscription) {
		return subscriptionCatalog{Subscriptions: []Subscription{}}, "", nil
	}
	if err != nil {
		return subscriptionCatalog{}, "", err
	}
	if !strings.HasPrefix(raw, "{") {
		return subscriptionCatalog{Subscriptions: []Subscription{{ID: subscriptionID(raw), URL: raw, Active: true}}}, raw, nil
	}
	var catalog subscriptionCatalog
	if err := json.Unmarshal([]byte(raw), &catalog); err != nil {
		return subscriptionCatalog{}, "", errors.New("无法读取已保存的订阅记录")
	}
	if catalog.Subscriptions == nil {
		catalog.Subscriptions = []Subscription{}
	}
	return catalog, raw, nil
}

func (s *Subscriptions) save(catalog subscriptionCatalog) error {
	data, err := json.Marshal(catalog)
	if err != nil {
		return err
	}
	if err := s.urls.Put(string(data)); err != nil {
		return fmt.Errorf("无法保存订阅记录到 Keychain: %w", err)
	}
	return nil
}

func (s *Subscriptions) restore(raw string) {
	if raw == "" {
		_ = s.urls.Delete()
	} else {
		_ = s.urls.Put(raw)
	}
}

func (s *Subscriptions) List() ([]Subscription, error) {
	catalog, _, err := s.catalog()
	return catalog.Subscriptions, err
}

func (s *Subscriptions) preserveActive(catalog subscriptionCatalog) error {
	for _, subscription := range catalog.Subscriptions {
		if !subscription.Active || !s.profiles.Exists() {
			continue
		}
		id := subscription.ID
		if _, err := s.profiles.LoadSubscription(id); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		data, err := s.profiles.Load()
		if err != nil {
			return err
		}
		return s.profiles.SaveSubscription(id, string(data))
	}
	return nil
}

func (s *Subscriptions) Import(ctx context.Context, address string) error {
	data, info, err := s.fetch(ctx, address)
	if err != nil {
		return err
	}
	if err := validateSubscription(data); err != nil {
		return err
	}
	catalog, _, err := s.catalog()
	if err != nil {
		return err
	}
	if err := s.preserveActive(catalog); err != nil {
		return err
	}
	id, err := newSubscriptionID(catalog)
	if err != nil {
		return err
	}
	info.ID, info.URL, info.Active = id, address, false
	catalog.Subscriptions = append(catalog.Subscriptions, info)
	if err := s.profiles.SaveSubscription(id, string(data)); err != nil {
		return err
	}
	if err := s.save(catalog); err != nil {
		_ = s.profiles.DeleteSubscription(id)
		return err
	}
	return nil
}

func (s *Subscriptions) ImportLocal(data string) error {
	if _, err := Compile([]byte(data), 7890, 9090, "validation-secret"); err != nil {
		return err
	}
	catalog, raw, err := s.catalog()
	if err != nil {
		return err
	}
	if err := s.preserveActive(catalog); err != nil {
		return err
	}
	for i := range catalog.Subscriptions {
		catalog.Subscriptions[i].Active = false
	}
	if err := s.save(catalog); err != nil {
		return err
	}
	if err := s.profiles.Import(data); err != nil {
		s.restore(raw)
		return err
	}
	return nil
}

func (s *Subscriptions) Update(ctx context.Context, id string) error {
	catalog, raw, err := s.catalog()
	if err != nil {
		return err
	}
	if err := s.preserveActive(catalog); err != nil {
		return err
	}
	index := -1
	for i := range catalog.Subscriptions {
		if catalog.Subscriptions[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return ErrNoSubscription
	}
	data, info, err := s.fetch(ctx, catalog.Subscriptions[index].URL)
	if err != nil {
		return err
	}
	if err := validateSubscription(data); err != nil {
		return err
	}
	for i := range catalog.Subscriptions {
		catalog.Subscriptions[i].Active = i == index
	}
	info.ID, info.URL, info.Active = id, catalog.Subscriptions[index].URL, true
	catalog.Subscriptions[index] = info
	if err := s.profiles.SaveSubscription(id, string(data)); err != nil {
		return err
	}
	if err := s.save(catalog); err != nil {
		return err
	}
	if err := s.profiles.Import(string(data)); err != nil {
		s.restore(raw)
		return err
	}
	return nil
}

func (s *Subscriptions) Select(ctx context.Context, id string) error {
	catalog, raw, err := s.catalog()
	if err != nil {
		return err
	}
	index := -1
	for i := range catalog.Subscriptions {
		if catalog.Subscriptions[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return ErrNoSubscription
	}
	if err := s.preserveActive(catalog); err != nil {
		return err
	}
	if catalog.Subscriptions[index].Active {
		return nil
	}
	data, err := s.profiles.LoadSubscription(id)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return s.Update(ctx, id)
	}
	for i := range catalog.Subscriptions {
		catalog.Subscriptions[i].Active = i == index
	}
	if err := s.save(catalog); err != nil {
		return err
	}
	if err := s.profiles.Import(string(data)); err != nil {
		s.restore(raw)
		return err
	}
	return nil
}

func validateSubscription(data []byte) error {
	if _, err := Compile(data, 7890, 9090, "validation-secret"); err != nil {
		if isBase64NodeList(data) {
			return errors.New("订阅服务器返回了 Base64 节点列表，请使用 Clash/Mihomo 格式的订阅")
		}
		return err
	}
	return nil
}

func isBase64NodeList(data []byte) bool {
	encoded := strings.Join(strings.Fields(string(data)), "")
	if encoded == "" {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(decoded), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, scheme := range []string{"ss://", "ssr://", "vmess://", "vless://", "trojan://", "hysteria://", "hysteria2://", "tuic://"} {
			if strings.HasPrefix(line, scheme) {
				return true
			}
		}
		return false
	}
	return false
}

var ErrNoSubscription = errors.New("尚未保存订阅地址")

func subscriptionInfo(header http.Header) Subscription {
	now := time.Now().UTC()
	info := Subscription{UpdatedAt: &now}
	for _, item := range strings.Split(header.Get("Subscription-Userinfo"), ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(item), "=")
		if !ok {
			continue
		}
		number, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || number < 0 {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "upload":
			info.Upload = &number
		case "download":
			info.Download = &number
		case "total":
			info.Total = &number
		case "expire":
			if number > 0 {
				expiry := time.Unix(number, 0).UTC()
				info.ExpiresAt = &expiry
			}
		}
	}
	return info
}

func (s *Subscriptions) fetch(ctx context.Context, address string) ([]byte, Subscription, error) {
	if len(address) == 0 || len(address) > 4096 {
		return nil, Subscription{}, errors.New("订阅地址长度无效")
	}
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, Subscription{}, errors.New("订阅地址必须是有效的 HTTP 或 HTTPS URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, Subscription{}, errors.New("无法创建订阅请求")
	}
	request.Header.Set("Accept", "application/yaml, text/yaml, text/plain, */*")
	request.Header.Set("User-Agent", "Clash.Meta")
	response, err := s.client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, Subscription{}, errors.New("订阅下载超时或已取消")
		}
		return nil, Subscription{}, errors.New("订阅下载失败，请检查地址和网络连接")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, Subscription{}, fmt.Errorf("订阅服务器返回 HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxConfigSize+1))
	if err != nil {
		return nil, Subscription{}, errors.New("读取订阅内容失败")
	}
	if len(data) > MaxConfigSize {
		return nil, Subscription{}, errors.New("订阅内容超过 2 MiB")
	}
	return data, subscriptionInfo(response.Header), nil
}
