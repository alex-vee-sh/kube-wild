package main

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"
)

type NameRef struct {
	Namespace          string
	Name               string
	CreatedAt          time.Time
	PodReasons         []string
	PodPhase           string
	Labels             map[string]string
	Annotations        map[string]string
	NodeName           string
	TotalRestarts      int
	NotReadyContainers int
	ReasonsByContainer map[string][]string
	Owners             []string // Kind/Name pairs like Deployment/web-1
}

type Matcher struct {
	Mode       MatchMode
	Includes   []string
	Excludes   []string
	IgnoreCase bool
	// Pre-compiled regexes for include/exclude patterns (when Mode == MatchRegex)
	IncludeRegexes []*regexp.Regexp
	ExcludeRegexes []*regexp.Regexp
	// Namespace filters
	NsExact  []string
	NsPrefix []string
	NsRegex  []*regexp.Regexp // Pre-compiled regexes
	// Fuzzy
	FuzzyMaxDistance int

	// Label filters
	LabelFilters     []LabelFilter
	LabelKeyRegex    []*regexp.Regexp // Pre-compiled regexes

	// Annotation filters (reuse LabelFilter type)
	AnnotationFilters  []LabelFilter
	AnnotationKeyRegex []*regexp.Regexp // Pre-compiled regexes
}

type LabelMode int

const (
	LabelGlob LabelMode = iota
	LabelPrefix
	LabelContains
	LabelRegex
)

type LabelFilter struct {
	Key           string
	Pattern       string
	Mode          LabelMode
	CompiledRegex *regexp.Regexp // Pre-compiled regex for LabelRegex mode (nil if not regex mode)
}

func parseLabelKV(kv string, mode LabelMode) (LabelFilter, error) {
	parts := strings.SplitN(kv, "=", 2)
	if len(parts) != 2 || parts[0] == "" {
		return LabelFilter{}, fmt.Errorf("label filter requires key=value: %s", kv)
	}
	return LabelFilter{Key: parts[0], Pattern: parts[1], Mode: mode}, nil
}

func labelValueMatches(value string, lf LabelFilter) bool {
	switch lf.Mode {
	case LabelGlob:
		ok, _ := path.Match(lf.Pattern, value)
		return ok
	case LabelPrefix:
		return strings.HasPrefix(value, lf.Pattern)
	case LabelContains:
		return strings.Contains(value, lf.Pattern)
	case LabelRegex:
		if lf.CompiledRegex != nil {
			return lf.CompiledRegex.MatchString(value)
		}
		// Fallback: compile on demand (shouldn't happen if pre-compiled properly)
		re := regexp.MustCompile(lf.Pattern)
		return re.MatchString(value)
	default:
		ok, _ := path.Match(lf.Pattern, value)
		return ok
	}
}

// LabelsAllowed applies AND across different keys, and OR across multiple filters of the same key.
func (m Matcher) LabelsAllowed(labels map[string]string) bool {
	if len(m.LabelFilters) == 0 {
		// If there are key-regex filters, require presence of at least one matching key per regex
		if len(m.LabelKeyRegex) == 0 {
			return true
		}
	}
	// group filters by key
	byKey := map[string][]LabelFilter{}
	for _, lf := range m.LabelFilters {
		byKey[lf.Key] = append(byKey[lf.Key], lf)
	}
	for key, fls := range byKey {
		val, ok := labels[key]
		if !ok {
			return false
		}
		matchedAny := false
		for _, f := range fls {
			if labelValueMatches(val, f) {
				matchedAny = true
				break
			}
		}
		if !matchedAny {
			return false
		}
	}
	// Key-regex presence checks (AND across regexes)
	for _, re := range m.LabelKeyRegex {
		found := false
		for k := range labels {
			if re.MatchString(k) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// AnnotationsAllowed applies AND across different keys, and OR across multiple filters of the same key.
// Same logic as LabelsAllowed but for annotations.
func (m Matcher) AnnotationsAllowed(annotations map[string]string) bool {
	if len(m.AnnotationFilters) == 0 {
		// If there are key-regex filters, require presence of at least one matching key per regex
		if len(m.AnnotationKeyRegex) == 0 {
			return true
		}
	}
	// group filters by key
	byKey := map[string][]LabelFilter{}
	for _, af := range m.AnnotationFilters {
		byKey[af.Key] = append(byKey[af.Key], af)
	}
	for key, fls := range byKey {
		val, ok := annotations[key]
		if !ok {
			return false
		}
		matchedAny := false
		for _, f := range fls {
			if labelValueMatches(val, f) {
				matchedAny = true
				break
			}
		}
		if !matchedAny {
			return false
		}
	}
	// Key-regex presence checks (AND across regexes)
	for _, re := range m.AnnotationKeyRegex {
		found := false
		for k := range annotations {
			if re.MatchString(k) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (m Matcher) Matches(name string) bool {
	n := name
	if m.IgnoreCase {
		n = strings.ToLower(n)
	}

	// includes
	if len(m.Includes) > 0 {
		matched := false
		for i, inc := range m.Includes {
			if m.Mode == MatchRegex && len(m.IncludeRegexes) > i && m.IncludeRegexes[i] != nil {
				// Use pre-compiled regex
				if m.IncludeRegexes[i].MatchString(n) {
					matched = true
					break
				}
			} else {
				if matchSingleWithDistance(m.Mode, m.IgnoreCase, n, inc, m.FuzzyMaxDistance) {
					matched = true
					break
				}
			}
		}
		if !matched {
			return false
		}
	}

	// excludes
	for i, exc := range m.Excludes {
		if m.Mode == MatchRegex && len(m.ExcludeRegexes) > i && m.ExcludeRegexes[i] != nil {
			// Use pre-compiled regex
			if m.ExcludeRegexes[i].MatchString(n) {
				return false
			}
		} else {
			if matchSingleWithDistance(m.Mode, m.IgnoreCase, n, exc, m.FuzzyMaxDistance) {
				return false
			}
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
		if re.MatchString(ns) {
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
		// Note: This function is now only called when regexes aren't pre-compiled
		// (e.g., for fuzzy mode or when pre-compilation wasn't done)
		// Pre-compiled regexes are used directly in Matches() method
		if ignoreCase {
			re := regexp.MustCompile("(?i)" + pattern)
			return re.MatchString(target)
		}
		re := regexp.MustCompile(pattern)
		return re.MatchString(target)
	case MatchContains:
		return strings.Contains(target, p)
	case MatchFuzzy:
		return fuzzyContains(target, pattern, 1, ignoreCase)
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
	return fuzzyContains(target, pattern, dist, ignoreCase)
}

// fuzzyContains matches pattern against target allowing up to dist edits.
// In addition to full-string distance, it attempts token-prefix and sliding-window
// matches so that patterns like "apu-1" can match pod names like "api-1-abc123".
func fuzzyContains(target string, pattern string, dist int, ignoreCase bool) bool {
	if pattern == "" {
		return true
	}
	t := target
	p := pattern
	if ignoreCase {
		t = strings.ToLower(t)
		p = strings.ToLower(p)
	}
	if levenshtein(t, p) <= dist {
		return true
	}
	// Token-based checks (split by common pod delimiters)
	delims := func(r rune) bool { return r == '-' || r == '_' || r == '.' }
	tokens := strings.FieldsFunc(t, delims)
	if len(tokens) > 0 {
		// Check cumulative prefixes of tokens (e.g., "api-1")
		var cumulative string
		for i, tok := range tokens {
			if i == 0 {
				cumulative = tok
			} else {
				cumulative = cumulative + "-" + tok
			}
			if levenshtein(cumulative, p) <= dist {
				return true
			}
			if levenshtein(tok, p) <= dist {
				return true
			}
			// Note: intentionally avoid arbitrary sliding windows to reduce false positives
			// like matching "pending-forever" for pattern "ngin".
		}
	}
	return false
}

// fuzzyWindow removed to avoid over-matching arbitrary inner substrings.

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
