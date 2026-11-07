package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Rule is a static injection for one exact path, loaded once at startup (the
// stateless counterpart of per-request parameters, for clients that can't
// change their URL or headers). Delay and Status use the same syntax as the
// sinkDelay and sinkStatus parameters, including probability lists.
type Rule struct {
	Path   string `json:"path"`
	Delay  string `json:"delay,omitempty"`
	Status string `json:"status,omitempty"`
}

type rulesFile struct {
	Rules []Rule `json:"rules"`
}

type ruleEntry struct {
	inj injection
}

// LoadRulesFile reads a rules file:
//
//	{"rules": [{"path": "/sink/orders", "delay": "50ms-300ms:20", "status": "503:10,429:5"}]}
func LoadRulesFile(name string) ([]Rule, error) {
	raw, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f rulesFile
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	return f.Rules, nil
}

// SetRules validates and installs the static rules. Call it before the server
// starts handling requests; the rules are read-only afterwards, so instances
// started from the same file behave identically and share no state.
func (s *Server) SetRules(rules []Rule) error {
	entries := make(map[string]ruleEntry, len(rules))
	list := make([]Rule, 0, len(rules))
	for i, r := range rules {
		where := fmt.Sprintf("rule %d (%q)", i+1, r.Path)
		if r.Path != "/sink" && !strings.HasPrefix(r.Path, "/sink/") {
			return fmt.Errorf("%s: path must be /sink or a subpath of /sink/", where)
		}
		if isControlPlane(r.Path) {
			return fmt.Errorf("%s: path is reserved by the sink's own endpoints", where)
		}
		if _, dup := entries[r.Path]; dup {
			return fmt.Errorf("%s: duplicate path", where)
		}
		if r.Delay == "" && r.Status == "" {
			return fmt.Errorf("%s: needs a delay and/or a status", where)
		}
		var inj injection
		var err error
		if r.Delay != "" {
			if inj.delays, err = parseDelays(r.Delay, s.opts.MaxDelay); err != nil {
				return fmt.Errorf("%s: delay %s: %v", where, delayHint, err)
			}
		}
		if r.Status != "" {
			if inj.statuses, err = parseStatuses(r.Status); err != nil {
				return fmt.Errorf("%s: status %s: %v", where, statusHint, err)
			}
		}
		entries[r.Path] = ruleEntry{inj: inj}
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Path < list[j].Path })
	s.rules, s.ruleList = entries, list
	return nil
}
