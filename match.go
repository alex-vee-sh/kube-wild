package main

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Pool for levenshtein row slices to reduce allocations in fuzzy matching hot path
var levenshteinPool = sync.Pool{
	New: func() interface{} {
		// Start with capacity 64, will grow as needed
		return &levenshteinRows{
			prev: make([]int, 64),
			curr: make([]int, 64),
		}
	},
}

type levenshteinRows struct {
	prev, curr []int
}

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
	// Pre-computed: map for fast exact namespace lookup (only populated if many exact namespaces)
	NsExactMap map[string]bool
	// Fuzzy
	FuzzyMaxDistance int

	// Label filters
	LabelFilters  []LabelFilter
	LabelKeyRegex []*regexp.Regexp // Pre-compiled regexes
	// Pre-computed: true if label filters have duplicate keys (needs grouping)
	LabelFiltersHaveDuplicates bool
	// Pre-computed grouped label filters (only populated if duplicates exist)
	LabelFiltersByKey map[string][]LabelFilter

	// Annotation filters (reuse LabelFilter type)
	AnnotationFilters  []LabelFilter
	AnnotationKeyRegex []*regexp.Regexp // Pre-compiled regexes
	// Pre-computed: true if annotation filters have duplicate keys (needs grouping)
	AnnotationFiltersHaveDuplicates bool
	// Pre-computed grouped annotation filters (only populated if duplicates exist)
	AnnotationFiltersByKey map[string][]LabelFilter
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
	// Accuracy: handle nil maps gracefully
	if labels == nil {
		// If filters require labels, nil means no match
		if len(m.LabelFilters) > 0 || len(m.LabelKeyRegex) > 0 {
			return false
		}
		return true
	}
	if len(m.LabelFilters) == 0 {
		// If there are key-regex filters, require presence of at least one matching key per regex
		if len(m.LabelKeyRegex) == 0 {
			return true
		}
	}
	// Fast path: each key appears only once, check directly (no map allocation)
	if !m.LabelFiltersHaveDuplicates {
		for _, lf := range m.LabelFilters {
			val, ok := labels[lf.Key]
			if !ok || !labelValueMatches(val, lf) {
				return false
			}
		}
	} else {
		// Slow path: use pre-computed grouped filters
		for key, fls := range m.LabelFiltersByKey {
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
	}
	// Key-regex presence checks (AND across regexes)
	// Optimize: check all regexes in a single pass through labels
	if len(m.LabelKeyRegex) > 0 {
		matchedRegexes := make([]bool, len(m.LabelKeyRegex))
		for k := range labels {
			for i, re := range m.LabelKeyRegex {
				if !matchedRegexes[i] && re.MatchString(k) {
					matchedRegexes[i] = true
				}
			}
			// Early exit: if all regexes matched, we're done
			allMatched := true
			for _, matched := range matchedRegexes {
				if !matched {
					allMatched = false
					break
				}
			}
			if allMatched {
				break
			}
		}
		// Check if all regexes matched
		for _, matched := range matchedRegexes {
			if !matched {
				return false
			}
		}
	}
	return true
}

// AnnotationsAllowed applies AND across different keys, and OR across multiple filters of the same key.
// Same logic as LabelsAllowed but for annotations.
func (m Matcher) AnnotationsAllowed(annotations map[string]string) bool {
	// Accuracy: handle nil maps gracefully
	if annotations == nil {
		// If filters require annotations, nil means no match
		if len(m.AnnotationFilters) > 0 || len(m.AnnotationKeyRegex) > 0 {
			return false
		}
		return true
	}
	if len(m.AnnotationFilters) == 0 {
		// If there are key-regex filters, require presence of at least one matching key per regex
		if len(m.AnnotationKeyRegex) == 0 {
			return true
		}
	}
	// Fast path: each key appears only once, check directly (no map allocation)
	if !m.AnnotationFiltersHaveDuplicates {
		for _, af := range m.AnnotationFilters {
			val, ok := annotations[af.Key]
			if !ok || !labelValueMatches(val, af) {
				return false
			}
		}
	} else {
		// Slow path: use pre-computed grouped filters
		for key, fls := range m.AnnotationFiltersByKey {
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
	}
	// Key-regex presence checks (AND across regexes)
	// Optimize: check all regexes in a single pass through annotations
	if len(m.AnnotationKeyRegex) > 0 {
		matchedRegexes := make([]bool, len(m.AnnotationKeyRegex))
		for k := range annotations {
			for i, re := range m.AnnotationKeyRegex {
				if !matchedRegexes[i] && re.MatchString(k) {
					matchedRegexes[i] = true
				}
			}
			// Early exit: if all regexes matched, we're done
			allMatched := true
			for _, matched := range matchedRegexes {
				if !matched {
					allMatched = false
					break
				}
			}
			if allMatched {
				break
			}
		}
		// Check if all regexes matched
		for _, matched := range matchedRegexes {
			if !matched {
				return false
			}
		}
	}
	return true
}

func (m Matcher) Matches(name string) bool {
	n := name
	if m.IgnoreCase {
		// Fast path: check if already lowercase to avoid allocation
		n = toLowerFast(n)
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
	// Optimize: use map for exact matches when available (O(1) vs O(n))
	if m.NsExactMap != nil {
		if m.NsExactMap[ns] {
			return true
		}
	} else {
		// Fallback: iterate for exact matches (when map not pre-computed)
		for _, e := range m.NsExact {
			if ns == e {
				return true
			}
		}
	}
	// Prefix matches
	for _, p := range m.NsPrefix {
		if strings.HasPrefix(ns, p) {
			return true
		}
	}
	// Regex matches
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
	// Performance: if target is empty and pattern is not, no match
	if target == "" {
		return false
	}
	t := target
	p := pattern
	if ignoreCase {
		t = strings.ToLower(t)
		p = strings.ToLower(p)
	}
	if levenshteinBounded(t, p, dist) <= dist {
		return true
	}
	// Token-based checks using manual iteration to avoid allocations
	// We find token boundaries and check levenshtein on substrings directly
	tLen := len(t)
	tokenStart := 0
	cumulativeEnd := 0
	firstToken := true

	for i := 0; i <= tLen; i++ {
		// Check if we hit a delimiter or end of string
		isDelim := i < tLen && (t[i] == '-' || t[i] == '_' || t[i] == '.')
		isEnd := i == tLen

		if isDelim || isEnd {
			if i > tokenStart {
				// Found a token from tokenStart to i
				tok := t[tokenStart:i]

				// Update cumulative end (including this token)
				if firstToken {
					cumulativeEnd = i
					firstToken = false
				} else {
					// Include the delimiter before this token in cumulative
					cumulativeEnd = i
				}

				// Check cumulative prefix (from start of string to current token end)
				cumulative := t[0:cumulativeEnd]
				if levenshteinBounded(cumulative, p, dist) <= dist {
					return true
				}

				// Check individual token
				if levenshteinBounded(tok, p, dist) <= dist {
					return true
				}
			}
			tokenStart = i + 1
		}
	}
	return false
}

// fuzzyWindow removed to avoid over-matching arbitrary inner substrings.

// levenshteinBounded computes edit distance but exits early if it exceeds maxDist.
// Returns the actual distance if <= maxDist, otherwise returns maxDist+1.
func levenshteinBounded(a, b string, maxDist int) int {
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
	// If length difference exceeds maxDist, no point computing
	diff := la - lb
	if diff < 0 {
		diff = -diff
	}
	if diff > maxDist {
		return maxDist + 1
	}
	// Get pooled rows to avoid allocations
	rows := levenshteinPool.Get().(*levenshteinRows)
	needed := lb + 1
	if cap(rows.prev) < needed {
		rows.prev = make([]int, needed)
		rows.curr = make([]int, needed)
	}
	prev := rows.prev[:needed]
	curr := rows.curr[:needed]
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		ca := a[i-1]
		minInRow := curr[0]
		for j := 1; j <= lb; j++ {
			cost := 0
			if ca != b[j-1] {
				cost = 1
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			curr[j] = min3(del, ins, sub)
			if curr[j] < minInRow {
				minInRow = curr[j]
			}
		}
		// Early termination: if minimum in row exceeds maxDist, stop
		if minInRow > maxDist {
			rows.prev, rows.curr = prev, curr
			levenshteinPool.Put(rows)
			return maxDist + 1
		}
		prev, curr = curr, prev
	}
	result := prev[lb]
	rows.prev, rows.curr = prev, curr
	levenshteinPool.Put(rows)
	return result
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
	// Get pooled rows to avoid allocations
	rows := levenshteinPool.Get().(*levenshteinRows)
	// Ensure slices are large enough
	needed := lb + 1
	if cap(rows.prev) < needed {
		rows.prev = make([]int, needed)
		rows.curr = make([]int, needed)
	}
	prev := rows.prev[:needed]
	curr := rows.curr[:needed]
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
	result := prev[lb]
	// Return rows to pool (swap back if needed to maintain consistency)
	rows.prev, rows.curr = prev, curr
	levenshteinPool.Put(rows)
	return result
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

// toLowerFast returns lowercase string without allocation if already lowercase.
// This is a fast path optimization for the common case where pod/resource names
// are already lowercase (Kubernetes naming conventions).
func toLowerFast(s string) string {
	// Quick scan: if all bytes are already lowercase (or non-alpha), return as-is
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			// Found uppercase, need to convert
			return strings.ToLower(s)
		}
	}
	return s // Already lowercase, no allocation
}
