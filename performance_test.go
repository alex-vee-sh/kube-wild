package main

import (
	"regexp"
	"strings"
	"testing"
)

// Benchmark tests for performance-critical paths

func BenchmarkLabelsAllowed_NoFilters(b *testing.B) {
	matcher := Matcher{}
	labels := map[string]string{"app": "web", "env": "prod"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matcher.LabelsAllowed(labels)
	}
}

func BenchmarkLabelsAllowed_WithFilters(b *testing.B) {
	matcher := Matcher{
		LabelFilters: []LabelFilter{
			{Key: "app", Pattern: "web", Mode: LabelContains},
			{Key: "env", Pattern: "prod", Mode: LabelContains},
		},
		LabelFiltersHaveDuplicates: false,
	}
	labels := map[string]string{"app": "web", "env": "prod"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matcher.LabelsAllowed(labels)
	}
}

func BenchmarkLabelsAllowed_WithDuplicates(b *testing.B) {
	matcher := Matcher{
		LabelFilters: []LabelFilter{
			{Key: "app", Pattern: "web", Mode: LabelContains},
			{Key: "app", Pattern: "api", Mode: LabelContains},
		},
		LabelFiltersHaveDuplicates: true,
		LabelFiltersByKey: map[string][]LabelFilter{
			"app": {
				{Key: "app", Pattern: "web", Mode: LabelContains},
				{Key: "app", Pattern: "api", Mode: LabelContains},
			},
		},
	}
	labels := map[string]string{"app": "web"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matcher.LabelsAllowed(labels)
	}
}

func BenchmarkLabelsAllowed_NilMap(b *testing.B) {
	matcher := Matcher{
		LabelFilters: []LabelFilter{
			{Key: "app", Pattern: "web", Mode: LabelContains},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matcher.LabelsAllowed(nil)
	}
}

func BenchmarkLabelKeyRegex_SinglePass(b *testing.B) {
	matcher := Matcher{
		LabelKeyRegex: []*regexp.Regexp{
			regexp.MustCompile("^app"),
			regexp.MustCompile("^env"),
		},
	}
	labels := map[string]string{
		"app":     "web",
		"env":     "prod",
		"version": "v1",
		"region":  "us-east",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matcher.LabelsAllowed(labels)
	}
}

func BenchmarkNamespaceAllowed_ExactMap(b *testing.B) {
	matcher := Matcher{
		NsExact: []string{"default", "kube-system", "prod", "staging", "dev"},
		NsExactMap: map[string]bool{
			"default":     true,
			"kube-system": true,
			"prod":        true,
			"staging":     true,
			"dev":         true,
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matcher.NamespaceAllowed("prod")
	}
}

func BenchmarkNamespaceAllowed_ExactIteration(b *testing.B) {
	matcher := Matcher{
		NsExact:    []string{"default", "prod"},
		NsExactMap: nil, // Force iteration path
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matcher.NamespaceAllowed("prod")
	}
}

func BenchmarkFuzzyContains_WithBuilder(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fuzzyContains("api-1-abc123-def456", "api-1", 1, false)
	}
}

func BenchmarkFuzzyContains_EmptyTarget(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fuzzyContains("", "pattern", 1, false)
	}
}

func BenchmarkMatches_IgnoreCase(b *testing.B) {
	matcher := Matcher{
		Mode:       MatchGlob,
		Includes:   []string{"web-*"},
		IgnoreCase: true,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matcher.Matches("Web-1") // Uppercase - will allocate
	}
}

func BenchmarkMatches_IgnoreCase_AlreadyLower(b *testing.B) {
	matcher := Matcher{
		Mode:       MatchGlob,
		Includes:   []string{"web-*"},
		IgnoreCase: true,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matcher.Matches("web-1") // Already lowercase - no allocation
	}
}

func BenchmarkMatches_RegexPrecompiled(b *testing.B) {
	matcher := Matcher{
		Mode:     MatchRegex,
		Includes: []string{"^web-"},
		IncludeRegexes: []*regexp.Regexp{
			regexp.MustCompile("^web-"),
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matcher.Matches("web-1")
	}
}

// Benchmark filtering large resource lists
func BenchmarkFiltering_LargeList(b *testing.B) {
	// Simulate 10,000 resources
	refs := make([]NameRef, 10000)
	for i := range refs {
		refs[i] = NameRef{
			Name:      strings.Join([]string{"pod", strings.Repeat("x", i%100)}, "-"),
			Namespace: "default",
			Labels: map[string]string{
				"app": "web",
			},
		}
	}

	matcher := Matcher{
		Mode:     MatchGlob,
		Includes: []string{"pod-*"},
		LabelFilters: []LabelFilter{
			{Key: "app", Pattern: "web", Mode: LabelContains},
		},
		LabelFiltersHaveDuplicates: false,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matched := 0
		for _, r := range refs {
			if matcher.Matches(r.Name) && matcher.LabelsAllowed(r.Labels) {
				matched++
			}
		}
	}
}

// Benchmark string concatenation methods
func BenchmarkStringConcat_Plus(b *testing.B) {
	ns := "default"
	name := "pod-1"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ns + "/" + name
	}
}

func BenchmarkStringConcat_Builder(b *testing.B) {
	ns := "default"
	name := "pod-1"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var builder strings.Builder
		builder.Grow(len(ns) + 1 + len(name))
		builder.WriteString(ns)
		builder.WriteByte('/')
		builder.WriteString(name)
		_ = builder.String()
	}
}

// generateTestJSON creates a JSON list with n items for benchmarking
func generateTestJSON(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"apiVersion":"v1","kind":"List","items":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"metadata":{"name":"pod-`)
		b.WriteString(strings.Repeat("x", 20))
		b.WriteString(`","namespace":"default","creationTimestamp":"2024-01-01T00:00:00Z","labels":{"app":"web","env":"prod"},"annotations":{"desc":"test"}},"spec":{"nodeName":"node-1"},"status":{"phase":"Running","containerStatuses":[{"name":"main","ready":true,"restartCount":0,"state":{"running":{}}}]}}`)
	}
	b.WriteString("]}")
	return []byte(b.String())
}

func BenchmarkDiscoverNames_100Items(b *testing.B) {
	jsonData := generateTestJSON(100)
	fr := &fakeRunner{
		outputs: map[string]string{
			"get pods -o json": string(jsonData),
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = discoverNames(fr, "pods", nil)
	}
}

func BenchmarkDiscoverNames_1000Items(b *testing.B) {
	jsonData := generateTestJSON(1000)
	fr := &fakeRunner{
		outputs: map[string]string{
			"get pods -o json": string(jsonData),
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = discoverNames(fr, "pods", nil)
	}
}
