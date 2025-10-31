package main

import (
	"path"
	"regexp"
	"strings"
	"time"
)

type NameRef struct {
	Namespace  string
	Name       string
	CreatedAt  time.Time
	PodReasons []string
}

type Matcher struct {
	Mode       MatchMode
	Includes   []string
	Excludes   []string
	IgnoreCase bool
	// Namespace filters
	NsExact  []string
	NsPrefix []string
	NsRegex  []string
	// Fuzzy
	FuzzyMaxDistance int
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
            if matchSingleWithDistance(m.Mode, m.IgnoreCase, n, inc, m.FuzzyMaxDistance) {
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
        if matchSingleWithDistance(m.Mode, m.IgnoreCase, n, exc, m.FuzzyMaxDistance) {
			return false
		}
	}
	return true
}

func (m Matcher) NamespaceAllowed(ns string) bool {
	// if no filters, allow all
	if len(m.NsExact) == 0 && len(m.NsPrefix) == 0 && len(m.NsRegex) == 0 {
		return true
	}
	for _, e := range m.NsExact {
		if ns == e {
			return true
		}
	}
	for _, p := range m.NsPrefix {
		if strings.HasPrefix(ns, p) {
			return true
		}
	}
	for _, re := range m.NsRegex {
		r := regexp.MustCompile(re)
		if r.MatchString(ns) {
			return true
		}
	}
	return false
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
	case MatchFuzzy:
        if ignoreCase {
            return levenshtein(strings.ToLower(target), strings.ToLower(pattern)) <= 1
        }
        return levenshtein(target, pattern) <= 1
	default:
		ok, _ := path.Match(p, target)
		return ok
	}
}

func matchSingleWithDistance(mode MatchMode, ignoreCase bool, target string, pattern string, dist int) bool {
    if mode != MatchFuzzy {
        return matchSingle(mode, ignoreCase, target, pattern)
    }
    if dist <= 0 {
        dist = 1
    }
    if ignoreCase {
        return levenshtein(strings.ToLower(target), strings.ToLower(pattern)) <= dist
    }
    return levenshtein(target, pattern) <= dist
}

func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	// allocate 2 rows
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		ca := a[i-1]
		for j := 1; j <= lb; j++ {
			cost := 0
			if ca != b[j-1] {
				cost = 1
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			curr[j] = min3(del, ins, sub)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
