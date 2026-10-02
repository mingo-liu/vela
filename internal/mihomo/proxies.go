package mihomo

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/mingo-liu/vela/internal/profile"
)

type Group struct {
	Name    string   `json:"name"`
	Current string   `json:"current"`
	Options []string `json:"options"`
}

func (r *Runner) Groups() ([]Group, error) {
	r.mu.Lock()
	if r.state.Status != "running" {
		defer r.mu.Unlock()
		selectors, err := r.store.SelectorGroups()
		if err != nil {
			return nil, err
		}
		selected, err := r.store.SelectedOptions()
		if err != nil {
			return nil, err
		}
		groups := make([]Group, 0, len(selectors))
		for _, selector := range selectors {
			current := ""
			if len(selector.Options) > 0 {
				current = selector.Options[0]
			}
			for _, option := range selector.Options {
				if option == selected[selector.Name] {
					current = option
					break
				}
			}
			groups = append(groups, Group{Name: selector.Name, Current: current, Options: selector.Options})
		}
		sortGroups(groups)
		return groups, nil
	}
	controller := r.controllerReadSnapshot()
	r.mu.Unlock()
	groups, err := controller.controllerEndpoint.groups()
	if err := r.validateControllerRead(controller, err); err != nil {
		return nil, err
	}
	return groups, nil
}

// controllerGroups is called with the runner mutex held.
func (r *Runner) controllerGroups() ([]Group, error) {
	return (controllerEndpoint{client: r.client, apiPort: r.apiPort, secret: r.secret}).groups()
}

func (controller controllerEndpoint) groups() ([]Group, error) {
	var response struct {
		Proxies map[string]struct {
			Type string   `json:"type"`
			Now  string   `json:"now"`
			All  []string `json:"all"`
		} `json:"proxies"`
	}
	if err := controller.request(http.MethodGet, "/proxies", nil, &response); err != nil {
		return nil, err
	}
	groups := make([]Group, 0)
	for name, proxy := range response.Proxies {
		if proxy.Type == "Selector" {
			groups = append(groups, Group{Name: name, Current: proxy.Now, Options: proxy.All})
		}
	}
	sortGroups(groups)
	return groups, nil
}

func sortGroups(groups []Group) {
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Name == "GLOBAL" {
			return false
		}
		if groups[j].Name == "GLOBAL" {
			return true
		}
		return groups[i].Name < groups[j].Name
	})
}

func (r *Runner) NodeNames() ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.store.NodeNames()
}

func (r *Runner) Select(group, option string) error {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.Status != "running" {
		return r.store.SelectOption(group, option)
	}
	candidate, err := r.controllerSelection(group, option)
	if err != nil {
		return err
	}
	if err := r.writeControllerSelection(group, option); err != nil {
		return err
	}
	if err := r.store.SelectOptionInGroup(group, option, candidate.Options); err != nil {
		if candidate.Current != "" {
			return errors.Join(err, r.writeControllerSelection(group, candidate.Current))
		}
		return err
	}
	return nil
}

// These controller helpers are called with the runner mutex held.
func (r *Runner) controllerSelection(group, option string) (Group, error) {
	groups, err := r.controllerGroups()
	if err != nil {
		return Group{}, err
	}
	for _, candidate := range groups {
		if candidate.Name == group {
			for _, name := range candidate.Options {
				if name == option {
					return candidate, nil
				}
			}
			return Group{}, errors.New("节点不在策略组中")
		}
	}
	return Group{}, errors.New("策略组不可手动选择")
}

func (r *Runner) restoreSelectedOptions() error {
	groups, err := r.controllerGroups()
	if err != nil {
		return err
	}
	selected, err := r.store.SelectedOptions()
	if err != nil {
		return err
	}
	effective := make([]profile.SelectorGroup, 0, len(groups))
	for _, group := range groups {
		effective = append(effective, profile.SelectorGroup{Name: group.Name, Options: group.Options})
		for _, option := range group.Options {
			if option == selected[group.Name] {
				if err := r.writeControllerSelection(group.Name, option); err != nil {
					return err
				}
				break
			}
		}
	}
	return r.store.ReconcileSelectedOptions(effective)
}

func (r *Runner) writeControllerSelection(group, option string) error {
	body, err := json.Marshal(map[string]string{"name": option})
	if err != nil {
		return err
	}
	return r.request(http.MethodPut, "/proxies/"+url.PathEscape(group), strings.NewReader(string(body)), nil)
}
