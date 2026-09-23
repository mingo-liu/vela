package profile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

const MaxConfigSize = 2 << 20

type Store struct {
	path string
}

func NewStore(dataDir string) *Store {
	return &Store{path: filepath.Join(dataDir, "profile.yaml")}
}

func (s *Store) Import(data string) error {
	if _, err := Compile([]byte(data), 7890, 9090, "validation-secret"); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".profile-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.WriteString(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.path)
}

func (s *Store) Load() ([]byte, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("请先导入一份 YAML 配置")
	}
	if err != nil {
		return nil, err
	}
	if len(data) > MaxConfigSize {
		return nil, errors.New("配置不能超过 2 MiB")
	}
	return data, nil
}

func (s *Store) Exists() bool {
	_, err := os.Stat(s.path)
	return err == nil
}

// Compile preserves compatible user settings while taking ownership of every
// inbound and controller setting. This first slice accepts inline resources only.
func Compile(data []byte, mixedPort, controllerPort int, secret string) ([]byte, error) {
	if mixedPort < 1 || mixedPort > 65535 || controllerPort < 1 || controllerPort > 65535 || secret == "" {
		return nil, errors.New("受管端口或控制密钥无效")
	}
	if len(data) == 0 || len(data) > MaxConfigSize {
		return nil, fmt.Errorf("配置大小必须在 1 字节到 %d 字节之间", MaxConfigSize)
	}
	var doc yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("YAML 解析失败: %w", err)
	}
	var second yaml.Node
	if err := dec.Decode(&second); err != io.EOF {
		return nil, errors.New("只支持一份 YAML 文档")
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("YAML 顶层必须是对象")
	}
	root := doc.Content[0]
	if err := checkNodes(root); err != nil {
		return nil, err
	}
	allowed := map[string]bool{
		"proxies": true, "proxy-groups": true, "rules": true,
		"hosts": true, "dns": true, "ipv6": true, "log-level": true,
		"unified-delay": true, "tcp-concurrent": true,
	}
	managed := map[string]bool{}
	for _, key := range []string{
		"port", "socks-port", "redir-port", "tproxy-port", "mixed-port", "allow-lan", "bind-address", "mode",
		"external-controller", "external-controller-tls", "external-controller-unix", "external-controller-pipe",
		"external-controller-cors", "external-ui", "external-ui-url", "external-doh-server", "secret", "lan-allowed-ips", "lan-disallowed-ips",
	} {
		managed[key] = true
	}
	for _, key := range []string{"tun", "listeners", "proxy-providers", "rule-providers", "script", "sniffer"} {
		if value := lookup(root, key); value != nil && !isEmpty(value) {
			return nil, fmt.Errorf("首版暂不支持 %s，请使用内联节点与规则", key)
		}
	}
	for i := 0; i < len(root.Content); i += 2 {
		key := root.Content[i].Value
		if !allowed[key] && !managed[key] {
			return nil, fmt.Errorf("首版暂不支持顶层字段 %s", key)
		}
	}
	if dns := lookup(root, "dns"); dns != nil && dns.Kind == yaml.MappingNode {
		if listen := lookup(dns, "listen"); listen != nil && listen.Value != "" && listen.Value != "0" {
			return nil, errors.New("首版不支持配置 DNS 监听端口")
		}
	}
	if rules := lookup(root, "rules"); rules != nil && rules.Kind == yaml.SequenceNode {
		for _, rule := range rules.Content {
			if rule.Kind == yaml.ScalarNode {
				upper := strings.ToUpper(strings.TrimSpace(rule.Value))
				for _, prefix := range []string{"GEOIP,", "GEOSITE,", "RULE-SET,", "IP-ASN,"} {
					if strings.HasPrefix(upper, prefix) {
						return nil, fmt.Errorf("首版尚未打包规则资源: %s", prefix)
					}
				}
			}
		}
	}
	for key := range managed {
		remove(root, key)
	}
	set(root, "mixed-port", fmt.Sprint(mixedPort), "!!int")
	set(root, "allow-lan", "false", "!!bool")
	set(root, "bind-address", "127.0.0.1", "!!str")
	set(root, "mode", "rule", "!!str")
	set(root, "external-controller", fmt.Sprintf("127.0.0.1:%d", controllerPort), "!!str")
	set(root, "secret", secret, "!!str")
	return yaml.Marshal(&doc)
}

func checkNodes(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode {
		return errors.New("首版不支持 YAML 别名")
	}
	if n.Kind == yaml.MappingNode {
		seen := make(map[string]bool)
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i].Value
			if seen[key] {
				return fmt.Errorf("重复的 YAML 字段: %s", key)
			}
			seen[key] = true
		}
	}
	for _, child := range n.Content {
		if err := checkNodes(child); err != nil {
			return err
		}
	}
	return nil
}

func lookup(n *yaml.Node, key string) *yaml.Node {
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func isEmpty(n *yaml.Node) bool {
	return (n.Kind == yaml.SequenceNode || n.Kind == yaml.MappingNode) && len(n.Content) == 0 || n.Tag == "!!null"
}

func remove(n *yaml.Node, key string) {
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content = append(n.Content[:i], n.Content[i+2:]...)
			return
		}
	}
}

func set(n *yaml.Node, key, value, tag string) {
	n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value})
}
