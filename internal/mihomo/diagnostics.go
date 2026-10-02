package mihomo

import "errors"

type ConnectionMetadata struct {
	SourceIP        string `json:"sourceIP"`
	SourcePort      string `json:"sourcePort"`
	DestinationIP   string `json:"destinationIP"`
	DestinationPort string `json:"destinationPort"`
	Host            string `json:"host"`
	Network         string `json:"network"`
	Process         string `json:"process"`
	ProcessPath     string `json:"processPath"`
}

type Connection struct {
	ID          string             `json:"id"`
	Metadata    ConnectionMetadata `json:"metadata"`
	Upload      int64              `json:"upload"`
	Download    int64              `json:"download"`
	Start       string             `json:"start"`
	Chains      []string           `json:"chains"`
	Rule        string             `json:"rule"`
	RulePayload string             `json:"rulePayload"`
}

type ConnectionSnapshot struct {
	UploadTotal   int64        `json:"uploadTotal"`
	DownloadTotal int64        `json:"downloadTotal"`
	Total         int          `json:"total"`
	Connections   []Connection `json:"connections"`
}

type Rule struct {
	Index   int    `json:"index"`
	Type    string `json:"type"`
	Payload string `json:"payload"`
	Proxy   string `json:"proxy"`
}

func (r *Runner) Connections() (ConnectionSnapshot, error) {
	r.mu.Lock()
	if r.state.Status != "running" {
		r.mu.Unlock()
		return ConnectionSnapshot{}, errors.New("请先连接代理以查看连接")
	}
	controller := r.controllerReadSnapshot()
	r.mu.Unlock()
	var snapshot ConnectionSnapshot
	if err := r.readController(controller, "/connections", &snapshot); err != nil {
		return ConnectionSnapshot{}, err
	}
	snapshot.Total = len(snapshot.Connections)
	if len(snapshot.Connections) > 500 {
		snapshot.Connections = snapshot.Connections[:500]
	}
	return snapshot, nil
}

func (r *Runner) Rules() ([]Rule, error) {
	r.mu.Lock()
	if r.state.Status != "running" {
		r.mu.Unlock()
		return nil, errors.New("请先连接代理以查看规则")
	}
	controller := r.controllerReadSnapshot()
	r.mu.Unlock()
	var response struct {
		Rules []Rule `json:"rules"`
	}
	if err := r.readController(controller, "/rules", &response); err != nil {
		return nil, err
	}
	return response.Rules, nil
}
