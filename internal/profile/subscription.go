package profile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

type URLStore interface {
	Get() (string, error)
	Put(string) error
	Delete() error
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

func (s *Subscriptions) Import(ctx context.Context, address string) error {
	data, err := s.fetch(ctx, address)
	if err != nil {
		return err
	}
	if _, err := Compile(data, 7890, 9090, "validation-secret"); err != nil {
		return err
	}
	previous, previousErr := s.urls.Get()
	if previousErr != nil && !errors.Is(previousErr, ErrNoSubscription) {
		return previousErr
	}
	if err := s.urls.Put(address); err != nil {
		return fmt.Errorf("无法保存订阅地址到 Keychain: %w", err)
	}
	if err := s.profiles.Import(string(data)); err != nil {
		if previousErr == nil {
			_ = s.urls.Put(previous)
		} else {
			_ = s.urls.Delete()
		}
		return err
	}
	return nil
}

func (s *Subscriptions) ImportLocal(data string) error {
	if _, err := Compile([]byte(data), 7890, 9090, "validation-secret"); err != nil {
		return err
	}
	previous, previousErr := s.urls.Get()
	if previousErr != nil && !errors.Is(previousErr, ErrNoSubscription) {
		return previousErr
	}
	if err := s.urls.Delete(); err != nil {
		return err
	}
	if err := s.profiles.Import(data); err != nil {
		if previousErr == nil {
			_ = s.urls.Put(previous)
		}
		return err
	}
	return nil
}

func (s *Subscriptions) Update(ctx context.Context) error {
	address, err := s.urls.Get()
	if err != nil {
		return err
	}
	data, err := s.fetch(ctx, address)
	if err != nil {
		return err
	}
	return s.profiles.Import(string(data))
}

var ErrNoSubscription = errors.New("尚未保存订阅地址")

func (s *Subscriptions) fetch(ctx context.Context, address string) ([]byte, error) {
	if len(address) == 0 || len(address) > 4096 {
		return nil, errors.New("订阅地址长度无效")
	}
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("订阅地址必须是有效的 HTTP 或 HTTPS URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("无法创建订阅请求")
	}
	request.Header.Set("Accept", "application/yaml, text/yaml, text/plain, */*")
	request.Header.Set("User-Agent", "Vela/0.1")
	response, err := s.client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, errors.New("订阅下载超时或已取消")
		}
		return nil, errors.New("订阅下载失败，请检查地址和网络连接")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("订阅服务器返回 HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxConfigSize+1))
	if err != nil {
		return nil, errors.New("读取订阅内容失败")
	}
	if len(data) > MaxConfigSize {
		return nil, errors.New("订阅内容超过 2 MiB")
	}
	return data, nil
}
