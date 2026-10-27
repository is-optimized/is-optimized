package scanner

import (
	"bufio"
	"errors"
	"regexp"
	"strings"
)

const userAgent = "IsOptimizedBot"

type robotRule struct {
	path  string
	allow bool
}
type robotGroup struct {
	agents []string
	rules  []robotRule
}
type robots struct {
	groups   []robotGroup
	sitemaps []string
}

func parseRobots(body string) (robots, error) {
	var r robots
	var group robotGroup
	hasRules := false
	recognized := false
	nonCommentLine := false
	flush := func() {
		if len(group.agents) > 0 {
			r.groups = append(r.groups, group)
		}
		group = robotGroup{}
		hasRules = false
	}
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 4096), 128<<10)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.SplitN(scanner.Text(), "#", 2)[0])
		if line == "" {
			continue
		}
		nonCommentLine = true
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			return r, errors.New("malformed robots policy")
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		switch key {
		case "user-agent":
			recognized = true
			if hasRules {
				flush()
			}
			if val == "" {
				return r, errors.New("empty robots user agent")
			}
			group.agents = append(group.agents, strings.ToLower(val))
		case "allow", "disallow":
			recognized = true
			if len(group.agents) == 0 {
				return r, errors.New("robots rule without user agent")
			}
			hasRules = true
			if val != "" {
				if !strings.HasPrefix(val, "/") {
					return r, errors.New("invalid robots rule")
				}
				group.rules = append(group.rules, robotRule{val, key == "allow"})
			}
		case "sitemap":
			recognized = true
			r.sitemaps = append(r.sitemaps, val)
		}
	}
	if err := scanner.Err(); err != nil {
		return r, errors.New("robots policy exceeds parser limits")
	}
	flush()
	if !recognized && nonCommentLine {
		return r, errors.New("unrecognized robots policy")
	}
	return r, nil
}
func (r robots) allowed(path string) bool {
	bestAgent := -1
	var selected []robotRule
	for _, g := range r.groups {
		rank := -1
		for _, a := range g.agents {
			if a == "*" && rank < 0 {
				rank = 0
			}
			if a != "*" && strings.Contains(strings.ToLower(userAgent), a) && len(a) > rank {
				rank = len(a)
			}
		}
		if rank > bestAgent {
			selected = nil
			bestAgent = rank
		}
		if rank >= 0 && rank == bestAgent {
			selected = append(selected, g.rules...)
		}
	}
	longest := -1
	allow := true
	for _, rule := range selected {
		p := rule.path
		end := strings.HasSuffix(p, "$")
		if end {
			p = strings.TrimSuffix(p, "$")
		}
		parts := strings.Split(p, "*")
		for i := range parts {
			parts[i] = regexp.QuoteMeta(parts[i])
		}
		pattern := "^" + strings.Join(parts, ".*")
		if end {
			pattern += "$"
		}
		matched, _ := regexp.MatchString(pattern, path)
		weight := len(strings.ReplaceAll(p, "*", ""))
		if matched && (weight > longest || (weight == longest && rule.allow)) {
			longest = weight
			allow = rule.allow
		}
	}
	return allow
}
