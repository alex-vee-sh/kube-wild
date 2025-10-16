package main

import (
	"path"
	"regexp"
	"strings"
)

type NameRef struct {
	Namespace string
	Name      string
}

type Matcher struct {
	Mode       MatchMode
	Includes   []string
	Excludes   []string
	IgnoreCase bool
}

func (m Matcher) Matches(name string) bool {
	n := name
	if m.IgnoreCase {
		n = strings.ToLower(n)
	}

	// includes
	if len(m.Includes) > 0 {
		matched := false
		for _, inc := range m.Includes {
			if matchSingle(m.Mode, m.IgnoreCase, n, inc) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// excludes
	for _, exc := range m.Excludes {
		if matchSingle(m.Mode, m.IgnoreCase, n, exc) {
			return false
		}
	}
	return true
}

func matchSingle(mode MatchMode, ignoreCase bool, target string, pattern string) bool {
	p := pattern
	if ignoreCase {
		p = strings.ToLower(p)
	}
	switch mode {
	case MatchGlob:
		ok, _ := path.Match(p, target)
		return ok
	case MatchRegex:
		if ignoreCase {
			re := regexp.MustCompile("(?i)" + pattern)
			return re.MatchString(target)
		}
		re := regexp.MustCompile(pattern)
		return re.MatchString(target)
	case MatchContains:
		return strings.Contains(target, p)
	default:
		ok, _ := path.Match(p, target)
		return ok
	}
}
