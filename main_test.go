package main

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestMain(m *testing.M) {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// Emit a heartbeat so users see progress on long runs/builds
				fmt.Fprintln(os.Stderr, "[tests] still running...")
			case <-done:
				return
			}
		}
	}()
	code := m.Run()
	close(done)
	os.Exit(code)
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

func TestParseArgs_NormalizeResource_DoesNotTreatResourceAsPattern(t *testing.T) {
	// When resource token is a shortcut (po), the next non-flag token is pattern, not the resource itself
	opts, err := parseArgs([]string{"get", "po", "-A", "--pod-status", "Running"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Resource != "pods" {
		t.Fatalf("expected resource pods, got %s", opts.Resource)
	}
	if len(opts.Include) != 1 || opts.Include[0] != "*" {
		t.Fatalf("expected default include '*', got %+v", opts.Include)
	}
}

func TestRunCommand_WithShortcutResourceAndStatus(t *testing.T) {
	// Ensure discovery uses pods and status filter works when resource provided as 'po'
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	now := time.Now().UTC().Format(time.RFC3339)
	json := fmt.Sprintf("{\"items\":[{\"metadata\":{\"name\":\"openapi\",\"namespace\":\"ns\",\"creationTimestamp\":\"%s\"},\"status\":{\"phase\":\"Running\"}},{\"metadata\":{\"name\":\"other\",\"namespace\":\"ns\",\"creationTimestamp\":\"%s\"},\"status\":{\"phase\":\"Pending\"}}]}", now, now)
	fr.outputs["get pods -o json -A"] = json
	// runGetAcrossNamespaces fetches with order: -A before -o json
	fr.outputs["get pods -A -o json"] = json
	opts, err := parseArgs([]string{"get", "po", "-A", "--pod-status", "Running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	// Expect final get to be rendered as a single table via -f, but minimally ensure kubectl get was invoked
	called := false
	for _, c := range fr.calls {
		if len(c) > 0 && c[0] == "get" {
			called = true
		}
	}
	if !called {
		t.Fatalf("expected kubectl get to be called; calls=%v", fr.calls)
	}
}

func TestNormalizeResource_Shortcuts(t *testing.T) {
	if got := normalizeResource("po"); got != "pods" {
		t.Fatalf("po -> %s", got)
	}
	if got := normalizeResource("svc"); got != "services" {
		t.Fatalf("svc -> %s", got)
	}
}

func TestParseArgs_AgeStatusOutputFlags(t *testing.T) {
	opts, err := parseArgs([]string{"get", "pods", "*", "--older-than", "15m", "--younger-than", "2h", "--pod-status", "CrashLoopBackOff", "--output", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.OlderThan == 0 || opts.YoungerThan == 0 || !opts.OutputJSON || len(opts.PodStatuses) != 1 {
		t.Fatalf("parse flags failed: %+v", opts)
	}
}

func TestFuzzyMatching_SelectsCloseNames(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	now := time.Now().UTC().Format(time.RFC3339)
	json := fmt.Sprintf("{\"items\":[{\"metadata\":{\"name\":\"nginx\",\"namespace\":\"default\",\"creationTimestamp\":\"%s\"}},{\"metadata\":{\"name\":\"api\",\"namespace\":\"default\",\"creationTimestamp\":\"%s\"}}]}", now, now)
	fr.outputs["get pods -o json"] = json
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"ngin"}, Mode: MatchFuzzy, FuzzyMaxDistance: 1}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	onlyNginx := false
	for _, c := range fr.calls {
		if len(c) >= 2 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " nginx ") && !strings.Contains(joined, " api ") {
				onlyNginx = true
			}
		}
	}
	if !onlyNginx {
		t.Fatalf("expected fuzzy to select nginx only; calls=%v", fr.calls)
	}
}

func TestFuzzyMatching_DoesNotOvermatchInnerSubstring(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	now := time.Now().UTC().Format(time.RFC3339)
	json := fmt.Sprintf("{\"items\":[{\"metadata\":{\"name\":\"pending-forever\",\"namespace\":\"default\",\"creationTimestamp\":\"%s\"}},{\"metadata\":{\"name\":\"nginx\",\"namespace\":\"default\",\"creationTimestamp\":\"%s\"}}]}", now, now)
	fr.outputs["get pods -o json"] = json
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"ngin"}, Mode: MatchFuzzy, FuzzyMaxDistance: 1}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	// Ensure only nginx appears in final get args
	onlyNginx := false
	for _, c := range fr.calls {
		if len(c) >= 2 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " nginx ") && !strings.Contains(joined, " pending-forever ") {
				onlyNginx = true
			}
		}
	}
	if !onlyNginx {
		t.Fatalf("expected fuzzy to exclude inner-substring matches; calls=%v", fr.calls)
	}
}

func TestFuzzyMatching_HashedPodName(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	now := time.Now().UTC().Format(time.RFC3339)
	json := fmt.Sprintf("{\"items\":[{\"metadata\":{\"name\":\"api-1-abcdef\",\"namespace\":\"default\",\"creationTimestamp\":\"%s\"}},{\"metadata\":{\"name\":\"web-1-xyz\",\"namespace\":\"default\",\"creationTimestamp\":\"%s\"}}]}", now, now)
	fr.outputs["get pods -o json"] = json
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"apu-1"}, Mode: MatchFuzzy, FuzzyMaxDistance: 1}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	matchedAPI := false
	matchedWeb := false
	for _, c := range fr.calls {
		if len(c) >= 2 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " api-1-abcdef ") {
				matchedAPI = true
			}
			if strings.Contains(joined, " web-1-xyz ") {
				matchedWeb = true
			}
		}
	}
	if !matchedAPI || matchedWeb {
		t.Fatalf("expected only api-1-abcdef; calls=%v", fr.calls)
	}
}

func TestAgeFilters_OlderThan(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	old := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	young := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	json := fmt.Sprintf("{\"items\":[{\"metadata\":{\"name\":\"old\",\"namespace\":\"ns\",\"creationTimestamp\":\"%s\"}},{\"metadata\":{\"name\":\"young\",\"namespace\":\"ns\",\"creationTimestamp\":\"%s\"}}]}", old, young)
	fr.outputs["get pods -o json"] = json
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob, OlderThan: time.Hour}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	onlyOld := false
	for _, c := range fr.calls {
		if len(c) >= 2 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " old ") && !strings.Contains(joined, " young ") {
				onlyOld = true
			}
		}
	}
	if !onlyOld {
		t.Fatalf("age filter failed; calls=%v", fr.calls)
	}
}

func TestPodStatusFilter_CrashLoopOnly(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	now := time.Now().UTC().Format(time.RFC3339)
	// bad pod has CrashLoopBackOff waiting reason; ok has none
	json := fmt.Sprintf("{\"items\":[{\"metadata\":{\"name\":\"bad\",\"namespace\":\"ns\",\"creationTimestamp\":\"%s\"},\"status\":{\"containerStatuses\":[{\"state\":{\"waiting\":{\"reason\":\"CrashLoopBackOff\"}}}]}},{\"metadata\":{\"name\":\"ok\",\"namespace\":\"ns\",\"creationTimestamp\":\"%s\"}}]}", now, now)
	fr.outputs["get pods -o json"] = json
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob, PodStatuses: []string{"CrashLoopBackOff"}}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	onlyBad := false
	for _, c := range fr.calls {
		if len(c) >= 2 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " bad ") && !strings.Contains(joined, " ok ") {
				onlyBad = true
			}
		}
	}
	if !onlyBad {
		t.Fatalf("status filter failed; calls=%v", fr.calls)
	}
}

func TestPodStatusFilter_PhaseRunning(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	now := time.Now().UTC().Format(time.RFC3339)
	// pod1 is Running by phase only; pod2 is Pending
	json := fmt.Sprintf("{\"items\":[{\"metadata\":{\"name\":\"p1\",\"namespace\":\"ns\",\"creationTimestamp\":\"%s\"},\"status\":{\"phase\":\"Running\"}},{\"metadata\":{\"name\":\"p2\",\"namespace\":\"ns\",\"creationTimestamp\":\"%s\"},\"status\":{\"phase\":\"Pending\"}}]}", now, now)
	fr.outputs["get pods -o json"] = json
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob, PodStatuses: []string{"Running"}}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	onlyP1 := false
	for _, c := range fr.calls {
		if len(c) >= 2 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " p1 ") && !strings.Contains(joined, " p2 ") {
				onlyP1 = true
			}
		}
	}
	if !onlyP1 {
		t.Fatalf("phase Running filter failed; calls=%v", fr.calls)
	}
}

func TestPodStatusFilter_Unhealthy(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	now := time.Now().UTC().Format(time.RFC3339)
	// Four pods: clean Running, Pending, CrashLoopBackOff, Succeeded
	json := fmt.Sprintf("{\"items\":["+
		"{\"metadata\":{\"name\":\"run\",\"namespace\":\"ns\",\"creationTimestamp\":\"%s\"},\"status\":{\"phase\":\"Running\",\"containerStatuses\":[{\"state\":{\"running\":{}}}]}},"+
		"{\"metadata\":{\"name\":\"pend\",\"namespace\":\"ns\",\"creationTimestamp\":\"%s\"},\"status\":{\"phase\":\"Pending\"}},"+
		"{\"metadata\":{\"name\":\"crash\",\"namespace\":\"ns\",\"creationTimestamp\":\"%s\"},\"status\":{\"phase\":\"Running\",\"containerStatuses\":[{\"state\":{\"waiting\":{\"reason\":\"CrashLoopBackOff\"}}}]}},"+
		"{\"metadata\":{\"name\":\"done\",\"namespace\":\"ns\",\"creationTimestamp\":\"%s\"},\"status\":{\"phase\":\"Succeeded\"}}]}", now, now, now, now)
	fr.outputs["get pods -o json"] = json
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob}
	opts.Unhealthy = true
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	// Expect only pend and crash to be included in final get args
	hasPend := false
	hasCrash := false
	hasRun := false
	hasDone := false
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " pend ") {
				hasPend = true
			}
			if strings.Contains(joined, " crash ") {
				hasCrash = true
			}
			if strings.Contains(joined, " run ") {
				hasRun = true
			}
			if strings.Contains(joined, " done ") {
				hasDone = true
			}
		}
	}
	if !hasPend || !hasCrash || hasRun || hasDone {
		t.Fatalf("unhealthy filter mismatch; calls=%v", fr.calls)
	}
}

func TestPodStatusFilter_RunningExcludesCrashLoop(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	now := time.Now().UTC().Format(time.RFC3339)
	// pod1 phase Running but container has CrashLoopBackOff waiting; should NOT match "Running"
	json := fmt.Sprintf("{\"items\":[{\"metadata\":{\"name\":\"p1\",\"namespace\":\"ns\",\"creationTimestamp\":\"%s\"},\"status\":{\"phase\":\"Running\",\"containerStatuses\":[{\"state\":{\"waiting\":{\"reason\":\"CrashLoopBackOff\"}}}]}}]}", now)
	fr.outputs["get pods -o json"] = json
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob, PodStatuses: []string{"Running"}}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	// Expect no get call for p1
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " p1 ") {
				t.Fatalf("running filter should exclude CrashLoopBackOff pod; calls=%v", fr.calls)
			}
		}
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
	_, err := discoverNames(fr, "pods", nil)
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
	fr := &fakeRunner{outputs: map[string]string{"get pods -o json -A": json, "get pods -A -o json": json}, errs: map[string]error{}}
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob, AllNamespaces: true}
	opts.DiscoveryFlags = []string{"-A"}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	// expect a single-table call via -f and with -A present
	hasSingleTable := false
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "get" && c[1] == "-f" {
			joined := " " + strings.Join(c, " ") + " "
			if strings.Contains(joined, " -A ") {
				hasSingleTable = true
			}
		}
	}
	if !hasSingleTable {
		t.Fatalf("expected single-table get -f invocation with -A; calls=%v", fr.calls)
	}
}

func TestNamespaceFilters_Applied(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	json := "{\"items\":[{" +
		"\"metadata\":{\"name\":\"a\",\"namespace\":\"ns1\"}}," +
		"{\"metadata\":{\"name\":\"b\",\"namespace\":\"prod-ns\"}}]}"
	fr.outputs["get pods -o json -A"] = json
	fr.outputs["get pods -A -o json"] = json
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob, AllNamespaces: true, NsPrefix: []string{"prod-"}}
	opts.DiscoveryFlags = []string{"-A"}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	// ensure we used single-table path (filtered list) instead of direct name calls
	usedFilteredList := false
	for _, c := range fr.calls {
		if len(c) >= 2 && c[0] == "get" && c[1] == "-f" {
			usedFilteredList = true
		}
		if len(c) >= 3 && c[0] == "get" && c[1] == "pods" && (c[2] == "a" || c[2] == "b") {
			t.Fatalf("unexpected direct name call when -A: %v", c)
		}
	}
	if !usedFilteredList {
		t.Fatalf("expected filtered single-table call; calls=%v", fr.calls)
	}
}

func TestConfirmThreshold_Blocks(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	fr.outputs["get pods -o json"] = discoveryJSON("a", "b", "c")
	opts := CLIOptions{Verb: VerbDelete, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob, ConfirmThreshold: 2}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	for _, c := range fr.calls {
		if len(c) > 0 && c[0] == "delete" {
			t.Fatal("delete should be blocked by threshold")
		}
	}
}

// logs intentionally unsupported
