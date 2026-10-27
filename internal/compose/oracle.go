package compose

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrUnparseable means the dry-run oracle's output cannot be read: there are no JSON progress lines (an old compose
// printed `null`), or none of them is about the container asked about. The caller falls back to `config --hash`; it
// never reads this as "no drift".
var ErrUnparseable = errors.New("dry-run output could not be parsed")

// Line is one progress line of `--progress json`: {"id":"Container <name>","status":"Working|Done","text":"<action>"}.
type Line struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Text   string `json:"text"`
}

// Oracle is the parsed output of `--dry-run --progress json up -d --no-deps --pull never --no-build <svc>`.
type Oracle struct {
	Lines []Line
}

// ParseOracle reads compose's output (stdout and stderr together: compose prints progress on stderr) for the container
// with the given name. It returns ErrUnparseable when there is no line about that container.
func ParseOracle(out []byte, containerName string) (Oracle, error) {
	var o Oracle
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var l Line
		if json.Unmarshal(line, &l) != nil || l.ID == "" || l.Text == "" {
			continue
		}
		if l.about(containerName) {
			o.Lines = append(o.Lines, l)
		}
	}
	if len(o.Lines) == 0 {
		return Oracle{}, ErrUnparseable
	}
	return o, nil
}

// about reports whether the line is about the container: "Container <name>", also as the temporary "<id>_<name>" compose
// renames the old container to while it recreates it.
func (l Line) about(name string) bool {
	rest, ok := strings.CutPrefix(l.ID, "Container ")
	if !ok || name == "" {
		return false
	}
	rest = strings.TrimSpace(rest)
	return rest == name || strings.HasSuffix(rest, "_"+name)
}

// WouldRecreate reports whether compose's own decision for this container is to recreate it, which means its config or
// its image changed (plan §3.6 check 1: "a Recreate text for the service's container means drift").
func (o Oracle) WouldRecreate() bool {
	for _, l := range o.Lines {
		if strings.HasPrefix(l.Text, "Recreate") {
			return true
		}
	}
	return false
}

// Texts lists the actions compose printed for the container, in order, without repeats: for messages and tests.
func (o Oracle) Texts() []string {
	var out []string
	for _, l := range o.Lines {
		if len(out) == 0 || out[len(out)-1] != l.Text {
			out = append(out, l.Text)
		}
	}
	return out
}

// ParseConfigHash reads the output of `config --hash=<svc>`: one `<service> <hash>` line per service.
func ParseConfigHash(out []byte, service string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && f[0] == service {
			return f[1], nil
		}
	}
	return "", errors.New("config --hash printed no hash for service " + service)
}

// ParsePullPolicy reads services.<svc>.pull_policy from `config --format json`. Nothing else of the output is kept. A
// missing policy is "" (compose's default); a service that is not in the output is an error.
func ParsePullPolicy(out []byte, service string) (string, error) {
	var doc struct {
		Services map[string]struct {
			PullPolicy string `json:"pull_policy"`
		} `json:"services"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return "", fmt.Errorf("cannot read the rendered compose config: %w", ErrUnparseable)
	}
	svc, ok := doc.Services[service]
	if !ok {
		return "", fmt.Errorf("service %s is not in the rendered compose config: %w", service, ErrUnparseable)
	}
	return strings.TrimSpace(svc.PullPolicy), nil
}

// ParseDeclaredLabels reads services.<svc>.labels from `config --format json` and returns the keys that begin with
// "whalenurse.", sorted. Compose renders `labels:` as a map; the list form ("key=value") is read too. No value is kept.
func ParseDeclaredLabels(out []byte, service string) ([]string, error) {
	var doc struct {
		Services map[string]struct {
			Labels json.RawMessage `json:"labels"`
		} `json:"services"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, fmt.Errorf("cannot read the rendered compose config: %w", ErrUnparseable)
	}
	svc, ok := doc.Services[service]
	if !ok {
		return nil, fmt.Errorf("service %s is not in the rendered compose config: %w", service, ErrUnparseable)
	}
	var keys []string
	add := func(k string) {
		if k = strings.TrimSpace(k); strings.HasPrefix(k, "whalenurse.") {
			keys = append(keys, k)
		}
	}
	var asMap map[string]json.RawMessage
	var asList []string
	switch {
	case len(svc.Labels) == 0 || string(svc.Labels) == "null":
	case json.Unmarshal(svc.Labels, &asMap) == nil:
		for k := range asMap {
			add(k)
		}
	case json.Unmarshal(svc.Labels, &asList) == nil:
		for _, kv := range asList {
			k, _, _ := strings.Cut(kv, "=")
			add(k)
		}
	default:
		return nil, fmt.Errorf("the labels of service %s are not a map or a list: %w", service, ErrUnparseable)
	}
	slices.Sort(keys)
	return slices.Compact(keys), nil
}

// PullPolicyBlocksPull reports whether the service's pull_policy forbids a pull: `never` and `build` (plan v3.8: the pull
// rung respects them; `--policy always` only replaces missing and if_not_present).
func PullPolicyBlocksPull(policy string) bool {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "never", "build":
		return true
	}
	return false
}
