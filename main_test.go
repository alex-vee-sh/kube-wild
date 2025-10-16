package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeRunner struct {
	outputs map[string]string
	errs    map[string]error
	calls   [][]string
}

func (f *fakeRunner) key(args []string) string { return strings.Join(args, " ") }

func (f *fakeRunner) RunKubectl(args []string) error {
	f.calls = append(f.calls, append([]string{}, args...))
	if err, ok := f.errs[f.key(args)]; ok {
		return err
	}
	return nil
}

func (f *fakeRunner) CaptureKubectl(args []string) ([]byte, []byte, error) {
	f.calls = append(f.calls, append([]string{}, args...))
	if err, ok := f.errs[f.key(args)]; ok {
		return nil, []byte(err.Error()), err
	}
	if out, ok := f.outputs[f.key(args)]; ok {
		return []byte(out), nil, nil
	}
	return nil, nil, nil
}

func discoveryJSON(names ...string) string {
	// namespace is empty in this helper; tests can craft ns/name later
	var b strings.Builder
	b.WriteString("{\"items\":[")
	for i, n := range names {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("{\"metadata\":{\"name\":\"")
		b.WriteString(n)
		b.WriteString("\",\"namespace\":\"\"}}")
	}
	b.WriteString("]}")
	return b.String()
}

func TestParseArgs_Basic(t *testing.T) {
	opts, err := parseArgs([]string{"get", "pods", "a*", "-n", "default", "--", "-owide"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Verb != VerbGet || opts.Resource != "pods" {
		t.Fatal("verb/resource parse failed")
	}
	if len(opts.Include) != 1 || opts.Include[0] != "a*" {
		t.Fatal("include parse failed")
	}
	if opts.Namespace != "default" {
		t.Fatalf("namespace parse failed: %q", opts.Namespace)
	}
	if len(opts.ExtraFinal) != 1 || opts.ExtraFinal[0] != "-owide" {
		t.Fatal("extra final failed")
	}
}

func TestParseArgs_Defaults(t *testing.T) {
	// No resource, no pattern -> defaults to pods and match-all
	opts, err := parseArgs([]string{"get"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Resource != "pods" {
		t.Fatalf("expected default resource pods, got %s", opts.Resource)
	}
	if len(opts.Include) != 1 || opts.Include[0] == "" {
		t.Fatalf("expected default include, got %+v", opts.Include)
	}
}

func TestPrefixRemovesDefaultInclude(t *testing.T) {
	opts, err := parseArgs([]string{"get", "pods", "-p", "ngin"})
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.Include) != 1 || opts.Include[0] != "ngin*" {
		t.Fatalf("expected only ngin*, got %+v", opts.Include)
	}
}

func TestMatcher_Glob_IncludeExclude(t *testing.T) {
	m := Matcher{Mode: MatchGlob, Includes: []string{"a*", "*b"}, Excludes: []string{"ab?"}}
	if !m.Matches("ax") {
		t.Fatal("expected ax to match")
	}
	if m.Matches("abz") {
		t.Fatal("expected abz excluded by ab?")
	}
}

func TestRunCommand_Get_Batching(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	fr.outputs["get pods -o json"] = discoveryJSON("a1", "a2", "a3", "b1")
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"a*"}, Mode: MatchGlob, BatchSize: 2}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	// Expect two get batches
	var got [][]string
	for _, c := range fr.calls {
		if len(c) >= 2 && c[0] == "get" && c[1] == "pods" && len(c) > 2 && c[2] != "-o" {
			got = append(got, c)
		}
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 get batches, got %d: %v", len(got), got)
	}
}

func TestRunCommand_Delete_ConfirmDryRun(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	fr.outputs["get pods -o json"] = discoveryJSON("te1", "te2")
	// Dry-run should not call delete
	opts := CLIOptions{Verb: VerbDelete, Resource: "pods", Include: []string{"te*"}, Mode: MatchGlob, DryRun: true}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	for _, c := range fr.calls {
		if len(c) > 0 && c[0] == "delete" {
			t.Fatal("delete should not be called in dry-run")
		}
	}
}

func TestParseArgs_PluginFlags(t *testing.T) {
	opts, err := parseArgs([]string{"describe", "pods", "foo", "--regex", "--match", "bar", "--exclude", "baz", "--ignore-case", "--batch-size", "10", "-A"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Mode != MatchRegex || !opts.IgnoreCase || opts.BatchSize != 10 || !opts.AllNamespaces {
		t.Fatal("plugin flag parse failed")
	}
	if !reflect.DeepEqual(opts.Include, []string{"foo", "bar"}) {
		t.Fatalf("includes: %+v", opts.Include)
	}
	if !reflect.DeepEqual(opts.Exclude, []string{"baz"}) {
		t.Fatalf("excludes: %+v", opts.Exclude)
	}
}

func TestDiscover_ErrorSurface(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	fr.errs["get pods -o json"] = errors.New("boom")
	_, err := discoverNames(fr, "pods", nil, false)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatal("expected surfaced error")
	}
}

func TestNamespaceForwarding_FinalCalls(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	fr.outputs["get pods -o json -n default"] = discoveryJSON("a1")
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"a*"}, Mode: MatchGlob, Namespace: "default"}
	// discovery flags must include -n default to hit our fake output
	opts.DiscoveryFlags = []string{"-n", "default"}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range fr.calls {
		if len(c) >= 5 && c[0] == "get" && c[1] == "pods" && c[2] == "a1" && c[3] == "-n" && c[4] == "default" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected final get to include -n default; calls=%v", fr.calls)
	}
}

func TestRegexAndContainsMatching(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	fr.outputs["get pods -o json"] = discoveryJSON("api-1", "web-1", "db")
	// regex: ^(api|web)-
	opts := CLIOptions{Verb: VerbDescribe, Resource: "pods", Include: []string{"^(api|web)-"}, Mode: MatchRegex}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	// contains: "pi-" should select api-1 only
	fr.calls = nil
	opts = CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"pi-"}, Mode: MatchContains}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	// find a get call with only api-1 in args
	ok := false
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "get" && c[1] == "pods" {
			joined := strings.Join(c[2:], " ")
			if strings.Contains(joined, "api-1") && !strings.Contains(joined, "web-1") && !strings.Contains(joined, " db ") {
				ok = true
			}
		}
	}
	if !ok {
		t.Fatalf("expected contains match to select only api-1; calls=%v", fr.calls)
	}
}

func TestAllNamespaces_TargetsNsName(t *testing.T) {
	// craft discovery with namespaces
	json := "{\"items\":[{" +
		"\"metadata\":{\"name\":\"a\",\"namespace\":\"ns1\"}}," +
		"{\"metadata\":{\"name\":\"b\",\"namespace\":\"ns2\"}}]}"
	fr := &fakeRunner{outputs: map[string]string{"get pods -o json -A": json}, errs: map[string]error{}}
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob, AllNamespaces: true}
	opts.DiscoveryFlags = []string{"-A"}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	// expect per-namespace final calls with -n ns1 and -n ns2
	hasNs1 := false
	hasNs2 := false
	for _, c := range fr.calls {
		if len(c) >= 5 && c[0] == "get" && c[1] == "pods" && c[2] == "a" && c[3] == "-n" && c[4] == "ns1" {
			hasNs1 = true
		}
		if len(c) >= 5 && c[0] == "get" && c[1] == "pods" && c[2] == "b" && c[3] == "-n" && c[4] == "ns2" {
			hasNs2 = true
		}
	}
	if !hasNs1 || !hasNs2 {
		t.Fatalf("expected per-namespace calls; calls=%v", fr.calls)
	}
}

// logs intentionally unsupported
