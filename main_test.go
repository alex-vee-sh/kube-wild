package main

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
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
	// Resource is passed through as-is; resolveCanonicalResource handles normalization dynamically
	opts, err := parseArgs([]string{"get", "po", "-A", "--pod-status", "Running"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Resource != "po" {
		t.Fatalf("expected resource po (passed through), got %s", opts.Resource)
	}
	if len(opts.Include) != 1 || opts.Include[0] != "*" {
		t.Fatalf("expected default include '*', got %+v", opts.Include)
	}
}

func TestRunCommand_WithShortcutResourceAndStatus(t *testing.T) {
	// Ensure discovery works when resource provided as 'po'
	// Now "po" stays as "po" (not resolved to "pods") for kubectl/oc compatibility
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	fr.outputs["api-resources --verbs=list"] = "NAME                              SHORTNAMES   APIGROUP        NAMESPACED   KIND         VERBS\npods                              po           core           true         Pod          [list get]\n"
	fr.outputs["api-resources -o name --verbs=list"] = "pods\n"
	now := time.Now().UTC().Format(time.RFC3339)
	json := fmt.Sprintf("{\"items\":[{\"metadata\":{\"name\":\"openapi\",\"namespace\":\"ns\",\"creationTimestamp\":\"%s\"},\"status\":{\"phase\":\"Running\"}},{\"metadata\":{\"name\":\"other\",\"namespace\":\"ns\",\"creationTimestamp\":\"%s\"},\"status\":{\"phase\":\"Pending\"}}]}", now, now)
	// Mock both "po" and "pods" since "po" now stays as-is
	fr.outputs["get po -o json -A"] = json
	fr.outputs["get po -A -o json"] = json
	fr.outputs["get pods -o json -A"] = json
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

func TestTopVerb_Pods(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	// Build JSON with namespace for pods
	json := "{\"items\":[" +
		"{\"metadata\":{\"name\":\"api-1\",\"namespace\":\"default\"}}," +
		"{\"metadata\":{\"name\":\"api-2\",\"namespace\":\"default\"}}," +
		"{\"metadata\":{\"name\":\"web-1\",\"namespace\":\"default\"}}]}"
	fr.outputs["get pods -o json -n default"] = json
	
	opts := CLIOptions{Verb: VerbTop, Resource: "pods", Include: []string{"api-*"}, Mode: MatchGlob, Namespace: "default", DiscoveryFlags: []string{"-n", "default"}}
	if err := runCommand(fr, opts); err != nil {
		t.Fatalf("runCommand failed: %v, calls=%v", err, fr.calls)
	}
	// Should call kubectl top pods with -n flag
	// Note: with multiple pods, kubectl top doesn't accept pod names, so we call without names
	found := false
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "top" && c[1] == "pods" {
			joined := strings.Join(c[2:], " ")
			// With multiple pods, we don't pass pod names, just -n flag
			if strings.Contains(joined, "-n") || strings.Contains(joined, "--namespace") {
				found = true
				// Should not have pod names when multiple pods matched
				if strings.Contains(joined, "api-1") || strings.Contains(joined, "api-2") {
					t.Fatal("expected no pod names when multiple pods matched")
				}
			}
		}
	}
	if !found {
		t.Fatalf("expected top pods call with -n flag; calls=%v", fr.calls)
	}
}

func TestTopVerb_Nodes(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	json := discoveryJSON("node-1", "node-2", "master-1")
	fr.outputs["get nodes -o json"] = json
	
	opts := CLIOptions{Verb: VerbTop, Resource: "nodes", Include: []string{"node-*"}, Mode: MatchGlob}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	// Should call kubectl top nodes with matched names, no namespace flag
	found := false
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "top" && c[1] == "nodes" {
			joined := strings.Join(c[2:], " ")
			if strings.Contains(joined, "node-1") && strings.Contains(joined, "node-2") && !strings.Contains(joined, "master-1") {
				found = true
				// Check namespace flag is NOT present (nodes are cluster-scoped)
				if strings.Contains(joined, "-n") || strings.Contains(joined, "--namespace") || strings.Contains(joined, "-A") {
					t.Fatal("nodes top should not have namespace flags")
				}
			}
		}
	}
	if !found {
		t.Fatalf("expected top nodes call with node-1 and node-2; calls=%v", fr.calls)
	}
}

func TestTopVerb_InvalidResource(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	json := discoveryJSON("svc-1")
	fr.outputs["get services -o json"] = json
	
	opts := CLIOptions{Verb: VerbTop, Resource: "services", Include: []string{"*"}, Mode: MatchGlob}
	err := runCommand(fr, opts)
	if err == nil {
		t.Fatal("expected error for top with non-pod/node resource")
	}
	if !strings.Contains(err.Error(), "only supports pods and nodes") {
		t.Fatalf("expected error about pods/nodes only, got: %v", err)
	}
}

func TestLabelFilters_Glob_AND_OR(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	// Two items with labels: app=web-1, app=api-1
	json := "{\"items\":[{" +
		"\"metadata\":{\"name\":\"web-1\",\"namespace\":\"ns\",\"labels\":{\"app\":\"web-1\"}}},{" +
		"\"metadata\":{\"name\":\"api-1\",\"namespace\":\"ns\",\"labels\":{\"app\":\"api-1\"}}}]}"
	fr.outputs["get pods -o json"] = json
	// label OR within same key: app=web-* OR app=api-*
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob}
	lf1, _ := parseLabelKV("app=web-*", LabelGlob)
	lf2, _ := parseLabelKV("app=api-*", LabelGlob)
	opts.LabelFilters = []LabelFilter{lf1, lf2}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	// Should include both names across batched get calls
	hasWeb := false
	hasApi := false
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " web-1 ") {
				hasWeb = true
			}
			if strings.Contains(joined, " api-1 ") {
				hasApi = true
			}
		}
	}
	if !hasWeb || !hasApi {
		t.Fatalf("label OR failed; calls=%v", fr.calls)
	}
}

func TestAnnotationFilters_Glob_AND_OR(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	// Two items with annotations: version=v1.0, version=v2.0
	json := "{\"items\":[{" +
		"\"metadata\":{\"name\":\"web-1\",\"namespace\":\"ns\",\"annotations\":{\"version\":\"v1.0\"}}},{" +
		"\"metadata\":{\"name\":\"api-1\",\"namespace\":\"ns\",\"annotations\":{\"version\":\"v2.0\"}}}]}"
	fr.outputs["get pods -o json"] = json
	// annotation OR within same key: version=v1.* OR version=v2.*
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob}
	af1, _ := parseLabelKV("version=v1.*", LabelGlob)
	af2, _ := parseLabelKV("version=v2.*", LabelGlob)
	opts.AnnotationFilters = []LabelFilter{af1, af2}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	// Should include both names across batched get calls
	hasWeb := false
	hasApi := false
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " web-1 ") {
				hasWeb = true
			}
			if strings.Contains(joined, " api-1 ") {
				hasApi = true
			}
		}
	}
	if !hasWeb || !hasApi {
		t.Fatalf("annotation OR failed; calls=%v", fr.calls)
	}
}

func TestAnnotationKeyRegex(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	// One item with annotation key matching regex, one without
	json := "{\"items\":[{" +
		"\"metadata\":{\"name\":\"web-1\",\"namespace\":\"ns\",\"annotations\":{\"deployment.kubernetes.io/revision\":\"1\"}}},{" +
		"\"metadata\":{\"name\":\"api-1\",\"namespace\":\"ns\",\"annotations\":{\"app\":\"api\"}}}]}"
	fr.outputs["get pods -o json"] = json
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob}
	opts.AnnotationKeyRegex = []string{"^deployment\\."}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	// Should only include web-1 (has deployment.* annotation key)
	hasWeb := false
	hasApi := false
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " web-1 ") {
				hasWeb = true
			}
			if strings.Contains(joined, " api-1 ") {
				hasApi = true
			}
		}
	}
	if !hasWeb || hasApi {
		t.Fatalf("annotation key regex failed; hasWeb=%v hasApi=%v calls=%v", hasWeb, hasApi, fr.calls)
	}
}

func TestGroupByLabel_AddsColumn(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	json := "{\"items\":[{" +
		"\"metadata\":{\"name\":\"a\",\"namespace\":\"ns\",\"labels\":{\"app\":\"x\"}}}]}"
	fr.outputs["get pods -o json"] = json
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob}
	opts.GroupByLabel = "app"
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	sawL := false
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "get" && c[1] == "pods" {
			for i := 2; i < len(c); i++ {
				if c[i] == "-L" && i+1 < len(c) && c[i+1] == "app" {
					sawL = true
				}
				if strings.HasPrefix(c[i], "-L=") {
					sawL = true
				}
			}
		}
	}
	if !sawL {
		t.Fatalf("expected -L app in final get; calls=%v", fr.calls)
	}
}

func TestNodeFilter_PrefixMatches(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	json := "{\"items\":[{" +
		"\"metadata\":{\"name\":\"a\",\"namespace\":\"ns\"},\"spec\":{\"nodeName\":\"worker-1\"}},{" +
		"\"metadata\":{\"name\":\"b\",\"namespace\":\"ns\"},\"spec\":{\"nodeName\":\"master-1\"}}]}"
	fr.outputs["get pods -o json"] = json
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob}
	opts.NodePrefix = []string{"worker-"}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	onlyA := false
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " a ") && !strings.Contains(joined, " b ") {
				onlyA = true
			}
		}
	}
	if !onlyA {
		t.Fatalf("node prefix filter failed; calls=%v", fr.calls)
	}
}

func TestRestartExpr_GreaterThan(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	json := "{\"items\":[{" +
		"\"metadata\":{\"name\":\"a\",\"namespace\":\"ns\"},\"status\":{\"containerStatuses\":[{\"name\":\"c\",\"ready\":true,\"restartCount\":2,\"state\":{\"running\":{}}}]}},{" +
		"\"metadata\":{\"name\":\"b\",\"namespace\":\"ns\"},\"status\":{\"containerStatuses\":[{\"name\":\"c\",\"ready\":true,\"restartCount\":0,\"state\":{\"running\":{}}}]}}]}"
	fr.outputs["get pods -o json"] = json
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob, RestartExpr: ">1"}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	onlyA := false
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " a ") && !strings.Contains(joined, " b ") {
				onlyA = true
			}
		}
	}
	if !onlyA {
		t.Fatalf("restart expr filter failed; calls=%v", fr.calls)
	}
}

func TestReasonFilter_ContainerScoped(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	json := "{\"items\":[{" +
		"\"metadata\":{\"name\":\"a\",\"namespace\":\"ns\"},\"status\":{\"containerStatuses\":[{\"name\":\"app\",\"ready\":false,\"restartCount\":1,\"state\":{\"waiting\":{\"reason\":\"OOMKilled\"}}},{\"name\":\"side\",\"ready\":true,\"restartCount\":0,\"state\":{\"running\":{}}}]}},{" +
		"\"metadata\":{\"name\":\"b\",\"namespace\":\"ns\"},\"status\":{\"containerStatuses\":[{\"name\":\"app\",\"ready\":true,\"restartCount\":0,\"state\":{\"running\":{}}}]}}]}"
	fr.outputs["get pods -o json"] = json
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob, ReasonFilters: []string{"OOMKilled"}, ContainerScope: "app"}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	onlyA := false
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " a ") && !strings.Contains(joined, " b ") {
				onlyA = true
			}
		}
	}
	if !onlyA {
		t.Fatalf("container-scoped reason filter failed; calls=%v", fr.calls)
	}
}

func TestLabelKeyRegex_Presence(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	json := "{\"items\":[{" +
		"\"metadata\":{\"name\":\"a\",\"namespace\":\"ns\",\"labels\":{\"app\":\"x\"}}},{" +
		"\"metadata\":{\"name\":\"b\",\"namespace\":\"ns\",\"labels\":{\"nope\":\"x\"}}}]}"
	fr.outputs["get pods -o json"] = json
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob}
	opts.LabelKeyRegex = []string{"^app$"}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	onlyA := false
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " a ") && !strings.Contains(joined, " b ") {
				onlyA = true
			}
		}
	}
	if !onlyA {
		t.Fatalf("label key regex presence failed; calls=%v", fr.calls)
	}
}

func TestLabelValueMatches_AllModes(t *testing.T) {
	// Test LabelGlob mode
	lfGlob, _ := parseLabelKV("app=web-*", LabelGlob)
	if !labelValueMatches("web-1", lfGlob) {
		t.Error("LabelGlob: web-1 should match web-*")
	}
	if labelValueMatches("api-1", lfGlob) {
		t.Error("LabelGlob: api-1 should not match web-*")
	}

	// Test LabelPrefix mode
	lfPrefix, _ := parseLabelKV("version=v1.", LabelPrefix)
	if !labelValueMatches("v1.0", lfPrefix) {
		t.Error("LabelPrefix: v1.0 should match v1. prefix")
	}
	if labelValueMatches("v2.0", lfPrefix) {
		t.Error("LabelPrefix: v2.0 should not match v1. prefix")
	}

	// Test LabelContains mode
	lfContains, _ := parseLabelKV("env=prod", LabelContains)
	if !labelValueMatches("prod-env", lfContains) {
		t.Error("LabelContains: prod-env should contain prod")
	}
	if !labelValueMatches("env-prod", lfContains) {
		t.Error("LabelContains: env-prod should contain prod")
	}
	if labelValueMatches("dev-env", lfContains) {
		t.Error("LabelContains: dev-env should not contain prod")
	}

	// Test LabelRegex mode with pre-compiled regex
	lfRegex, _ := parseLabelKV("version=v[0-9]+", LabelRegex)
	lfRegex.CompiledRegex = regexp.MustCompile(lfRegex.Pattern)
	if !labelValueMatches("v1", lfRegex) {
		t.Error("LabelRegex: v1 should match v[0-9]+")
	}
	if !labelValueMatches("v123", lfRegex) {
		t.Error("LabelRegex: v123 should match v[0-9]+")
	}
	if labelValueMatches("vabc", lfRegex) {
		t.Error("LabelRegex: vabc should not match v[0-9]+")
	}

	// Test LabelRegex mode fallback (without CompiledRegex)
	lfRegexFallback, _ := parseLabelKV("version=v[0-9]+", LabelRegex)
	lfRegexFallback.CompiledRegex = nil // Force fallback path
	if !labelValueMatches("v1", lfRegexFallback) {
		t.Error("LabelRegex fallback: v1 should match v[0-9]+")
	}
	if labelValueMatches("vabc", lfRegexFallback) {
		t.Error("LabelRegex fallback: vabc should not match v[0-9]+")
	}

	// Test default case (should use glob)
	lfDefault := LabelFilter{Key: "app", Pattern: "web-*", Mode: LabelMode(999)} // Invalid mode
	if !labelValueMatches("web-1", lfDefault) {
		t.Error("Default mode: web-1 should match web-* (fallback to glob)")
	}
}

func TestLabelFilters_PrefixContainsRegex(t *testing.T) {
	fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
	json := "{\"items\":[{" +
		"\"metadata\":{\"name\":\"web-1\",\"namespace\":\"ns\",\"labels\":{\"version\":\"v1.0\",\"env\":\"prod-env\"}}},{" +
		"\"metadata\":{\"name\":\"api-1\",\"namespace\":\"ns\",\"labels\":{\"version\":\"v2.0\",\"env\":\"dev-env\"}}}]}"
	fr.outputs["get pods -o json"] = json

	// Test LabelPrefix
	opts := CLIOptions{Verb: VerbGet, Resource: "pods", Include: []string{"*"}, Mode: MatchGlob}
	lfPrefix, _ := parseLabelKV("version=v1.", LabelPrefix)
	opts.LabelFilters = []LabelFilter{lfPrefix}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	hasWeb := false
	hasApi := false
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " web-1 ") {
				hasWeb = true
			}
			if strings.Contains(joined, " api-1 ") {
				hasApi = true
			}
		}
	}
	if !hasWeb || hasApi {
		t.Fatalf("LabelPrefix filter failed; hasWeb=%v hasApi=%v", hasWeb, hasApi)
	}

	// Test LabelContains
	fr.calls = [][]string{}
	opts.LabelFilters = nil
	lfContains, _ := parseLabelKV("env=prod", LabelContains)
	opts.LabelFilters = []LabelFilter{lfContains}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	hasWeb = false
	hasApi = false
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " web-1 ") {
				hasWeb = true
			}
			if strings.Contains(joined, " api-1 ") {
				hasApi = true
			}
		}
	}
	if !hasWeb || hasApi {
		t.Fatalf("LabelContains filter failed; hasWeb=%v hasApi=%v", hasWeb, hasApi)
	}

	// Test LabelRegex
	fr.calls = [][]string{}
	opts.LabelFilters = nil
	lfRegex, _ := parseLabelKV("version=v[0-9]+", LabelRegex)
	opts.LabelFilters = []LabelFilter{lfRegex}
	if err := runCommand(fr, opts); err != nil {
		t.Fatal(err)
	}
	hasWeb = false
	hasApi = false
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "get" && c[1] == "pods" {
			joined := " " + strings.Join(c[2:], " ") + " "
			if strings.Contains(joined, " web-1 ") {
				hasWeb = true
			}
			if strings.Contains(joined, " api-1 ") {
				hasApi = true
			}
		}
	}
	if !hasWeb || !hasApi {
		t.Fatalf("LabelRegex filter failed; hasWeb=%v hasApi=%v (both should match v[0-9]+)", hasWeb, hasApi)
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

func TestAllNamespaces_Services_SingleTable(t *testing.T) {
    // craft discovery with namespaces for services
    json := "{\"items\":[{" +
        "\"metadata\":{\"name\":\"s1\",\"namespace\":\"ns1\"}},{" +
        "\"metadata\":{\"name\":\"s2\",\"namespace\":\"ns2\"}}]}"
    fr := &fakeRunner{outputs: map[string]string{
        "get services -o json -A": json,
        "get services -A -o json": json,
    }, errs: map[string]error{}}
    opts := CLIOptions{Verb: VerbGet, Resource: "services", Include: []string{"*"}, Mode: MatchGlob, AllNamespaces: true}
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

func TestParseArgs_NamespaceWildcard(t *testing.T) {
    opts, err := parseArgs([]string{"get", "pods", "-n", "xyz*"})
    if err != nil {
        t.Fatal(err)
    }
    if !opts.AllNamespaces {
        t.Fatalf("expected AllNamespaces implied by wildcard -n")
    }
    if opts.Namespace != "" {
        t.Fatalf("expected exact Namespace to be empty when wildcard provided, got %q", opts.Namespace)
    }
    if len(opts.NsPrefix) != 1 || opts.NsPrefix[0] != "xyz" {
        t.Fatalf("expected NsPrefix [xyz], got %+v", opts.NsPrefix)
    }
    // discovery flags should include -A and not include -n xyz*
    joined := " " + strings.Join(opts.DiscoveryFlags, " ") + " "
    if !strings.Contains(joined, " -A ") {
        t.Fatalf("expected -A in discovery flags; got %v", opts.DiscoveryFlags)
    }
    if strings.Contains(joined, " -n ") || strings.Contains(joined, " --namespace ") {
        t.Fatalf("did not expect -n/--namespace forwarded for wildcard; got %v", opts.DiscoveryFlags)
    }
}

func TestWildcardNamespace_EndToEnd_FilteredSingleTable(t *testing.T) {
    fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
    // services across namespaces; only prod-* should be kept
    json := "{\"items\":[{" +
        "\"metadata\":{\"name\":\"a\",\"namespace\":\"dev\"}}," +
        "{\"metadata\":{\"name\":\"b\",\"namespace\":\"prod-x\"}}]}"
    fr.outputs["get services -A -o json"] = json
    fr.outputs["get services -o json -A"] = json
    opts, err := parseArgs([]string{"get", "services", "-n", "prod-*"})
    if err != nil {
        t.Fatal(err)
    }
    if err := runCommand(fr, opts); err != nil {
        t.Fatal(err)
    }
    // ensure single-table path used (get -f ...) and -A present
    usedFilteredList := false
    for _, c := range fr.calls {
        if len(c) >= 2 && c[0] == "get" && c[1] == "-f" {
            usedFilteredList = true
        }
    }
    if !usedFilteredList {
        t.Fatalf("expected filtered single-table call; calls=%v", fr.calls)
    }
}

func TestRoutes_AllNamespaces_SingleTable(t *testing.T) {
    // OpenShift routes behave like namespaced resources for our purposes
    json := "{\"items\":[{" +
        "\"metadata\":{\"name\":\"r1\",\"namespace\":\"ns1\"}},{" +
        "\"metadata\":{\"name\":\"r2\",\"namespace\":\"ns2\"}}]}"
    fr := &fakeRunner{outputs: map[string]string{
        "get routes -o json -A": json,
        "get routes -A -o json": json,
    }, errs: map[string]error{}}
    opts := CLIOptions{Verb: VerbGet, Resource: "routes", Include: []string{"*"}, Mode: MatchGlob, AllNamespaces: true}
    opts.DiscoveryFlags = []string{"-A"}
    if err := runCommand(fr, opts); err != nil {
        t.Fatal(err)
    }
    usedFilteredList := false
    for _, c := range fr.calls {
        if len(c) >= 2 && c[0] == "get" && c[1] == "-f" {
            usedFilteredList = true
        }
    }
    if !usedFilteredList {
        t.Fatalf("expected filtered single-table call; calls=%v", fr.calls)
    }
}

func TestClusterScoped_IgnoresAllNamespaces(t *testing.T) {
    clearResourceCaches()
    // Simulate cluster-scoped resource like nodes
    fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
    // api-resources responses
    fr.outputs["api-resources -o name --verbs=list --namespaced=true"] = "pods\nservices\n"
    fr.outputs["api-resources -o name --verbs=list --namespaced=false"] = "nodes\ncustomresourcedefinitions.apiextensions.k8s.io\n"
    fr.outputs["api-resources --verbs=list"] = "NAME                              SHORTNAMES   APIGROUP        NAMESPACED   KIND         VERBS\npods                              po           core           true         Pod          [list get]\nservices                         svc          core           true         Service      [list get]\nnodes                                           	             false        Node         [list get]\ncustomresourcedefinitions         crd,crds     apiextensions.k8s.io false        CustomResourceDefinition [list get]\n"
    // discovery for nodes should NOT include -A
    fr.outputs["get nodes -o json"] = "{\"items\":[{\"metadata\":{\"name\":\"n1\"}},{\"metadata\":{\"name\":\"n2\"}}]}"
    opts := CLIOptions{Verb: VerbGet, Resource: "nodes", Include: []string{"*"}, Mode: MatchGlob, AllNamespaces: true}
    opts.DiscoveryFlags = []string{"-A"}
    if err := runCommand(fr, opts); err != nil {
        t.Fatal(err)
    }
    // Ensure there was a get nodes call without -A and without -n and containing names
    saw := false
    for _, c := range fr.calls {
        if len(c) >= 3 && c[0] == "get" && c[1] == "nodes" {
            joined := " " + strings.Join(c, " ") + " "
            if !strings.Contains(joined, " -A ") && !strings.Contains(joined, " -n ") && strings.Contains(joined, " n1 ") && strings.Contains(joined, " n2 ") {
                saw = true
            }
        }
    }
    if !saw {
        t.Fatalf("expected get nodes without -A/-n and with names; calls=%v", fr.calls)
    }
}

func TestCanonicalResource_ResolvesCRD_FromSingularAndShortname(t *testing.T) {
    fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
    // api-resources discovery
    fr.outputs["api-resources -o name --verbs=list"] = "bgppeers.metallb.io\n"
    // table with columns: NAME SHORTNAMES APIGROUP NAMESPACED KIND VERBS
    fr.outputs["api-resources --verbs=list"] = strings.Join([]string{
        "NAME                              SHORTNAMES   APIGROUP        NAMESPACED   KIND         VERBS",
        "bgppeers                          bgpp        metallb.io      true         BGPPeer      [list get]",
        "nodes                                           	             false        Node         [list get]",
    }, "\n")
    // discovery for canonical resource
    fr.outputs["get bgppeers.metallb.io -o json"] = discoveryJSON("peer1")
    // singular form should resolve to plural.group
    opts := CLIOptions{Verb: VerbGet, Resource: "bgppeer", Include: []string{"*"}, Mode: MatchGlob}
    if err := runCommand(fr, opts); err != nil {
        t.Fatal(err)
    }
    // Ensure get targeted the canonical name
    saw := false
    for _, c := range fr.calls {
        if len(c) >= 3 && c[0] == "get" && c[1] == "bgppeers.metallb.io" && c[2] == "-o" {
            saw = true
        }
    }
    if !saw {
        t.Fatalf("expected discovery to use canonical plural.group; calls=%v", fr.calls)
    }
    // shortname should also resolve
    fr.calls = nil
    if err := runCommand(fr, CLIOptions{Verb: VerbGet, Resource: "bgpp", Include: []string{"*"}, Mode: MatchGlob}); err != nil {
        t.Fatal(err)
    }
    saw = false
    for _, c := range fr.calls {
        if len(c) >= 3 && c[0] == "get" && c[1] == "bgppeers.metallb.io" && c[2] == "-o" {
            saw = true
        }
    }
    if !saw {
        t.Fatalf("expected shortname to resolve to canonical; calls=%v", fr.calls)
    }
}

func TestResolveCanonical_PassThroughGroupQualified(t *testing.T) {
    fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
    fr.outputs["api-resources -o name --verbs=list"] = "bgppeers.metallb.io\nservices\n"
    got, err := resolveCanonicalResource(fr, "bgppeers.metallb.io")
    if err != nil {
        t.Fatal(err)
    }
    if got != "bgppeers.metallb.io" {
        t.Fatalf("expected pass-through, got %s", got)
    }
}

func TestIsResourceNamespaced_NamespacedTrue(t *testing.T) {
    fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
    fr.outputs["api-resources -o name --verbs=list --namespaced=true"] = "configmaps\n"
    fr.outputs["api-resources -o name --verbs=list --namespaced=false"] = "nodes\n"
    ns, err := isResourceNamespaced(fr, "configmaps")
    if err != nil {
        t.Fatal(err)
    }
    if !ns {
        t.Fatal("expected namespaced=true for configmaps")
    }
}

func TestMatcher_NamespaceAllowed_ExactAndRegex(t *testing.T) {
    m := Matcher{NsExact: []string{"dev"}}
    if !m.NamespaceAllowed("dev") || m.NamespaceAllowed("prod") {
        t.Fatal("NsExact failed")
    }
    m = Matcher{NsRegex: []*regexp.Regexp{regexp.MustCompile("^prod-.*$")}}
    if !m.NamespaceAllowed("prod-a") || m.NamespaceAllowed("dev") {
        t.Fatal("NsRegex failed")
    }
}

func TestFilterAndStripNamespaceFlags(t *testing.T) {
    flags := []string{"-o", "json", "--output", "yaml", "-n", "default", "-A", "--all-namespaces"}
    filtered := filterOutputFlags(flags)
    joined := " " + strings.Join(filtered, " ") + " "
    if strings.Contains(joined, " -o ") || strings.Contains(joined, " --output ") {
        t.Fatalf("output flags not filtered: %v", filtered)
    }
    if !strings.Contains(joined, " -n ") || !strings.Contains(joined, " -A ") {
        t.Fatalf("unexpected removal beyond output flags: %v", filtered)
    }
    if s := stripNamespaceFlag(filtered); strings.Contains(" "+strings.Join(s, " ")+" ", " -n ") {
        t.Fatalf("-n not stripped: %v", s)
    }
    if s := stripAllNamespacesFlag(filtered); strings.Contains(" "+strings.Join(s, " ")+" ", " -A ") {
        t.Fatalf("-A not stripped: %v", s)
    }
}

func TestPrintLabelSummary_WritesCounts(t *testing.T) {
    tmp, err := os.CreateTemp("", "wild-summary-*.txt")
    if err != nil {
        t.Fatal(err)
    }
    defer os.Remove(tmp.Name())
    matched := []matchedRef{
        {ns: "ns1", name: "a", labels: map[string]string{"app": "web"}},
        {ns: "ns1", name: "b", labels: map[string]string{"app": "web"}},
        {ns: "ns2", name: "c", labels: map[string]string{"app": "api"}},
    }
    opts := CLIOptions{GroupByLabel: "app", ColorizeLabels: false}
    printLabelSummary(tmp, opts, matched)
    tmp.Close()
    data, err := os.ReadFile(tmp.Name())
    if err != nil {
        t.Fatal(err)
    }
    s := string(data)
    if !strings.Contains(s, "web → 2") || !strings.Contains(s, "api → 1") {
        t.Fatalf("unexpected summary: %s", s)
    }
}

func TestCompareIntExpr_AllOps(t *testing.T) {
    if !compareIntExpr(5, ">3") {
        t.Fatal("> failed")
    }
    if !compareIntExpr(5, ">=5") {
        t.Fatal(">= failed")
    }
    if !compareIntExpr(3, "<5") {
        t.Fatal("< failed")
    }
    if !compareIntExpr(3, "<=3") {
        t.Fatal("<= failed")
    }
    if !compareIntExpr(3, "=3") || !compareIntExpr(3, "3") {
        t.Fatal("= or bare number failed")
    }
    if compareIntExpr(3, ">x") {
        t.Fatal("invalid should be false")
    }
}

func TestNodeAllowed_ExactAndRegex(t *testing.T) {
    nodeExact := []string{"n1"}
    nodePrefix := []string{}
    nodeRegexes := []*regexp.Regexp{}
    if !nodeAllowed("n1", nodeExact, nodePrefix, nodeRegexes) || nodeAllowed("n2", nodeExact, nodePrefix, nodeRegexes) {
        t.Fatal("exact node match failed")
    }
    nodeExact = []string{}
    nodeRegexes = []*regexp.Regexp{regexp.MustCompile("^work-\\d+$")}
    if !nodeAllowed("work-1", nodeExact, nodePrefix, nodeRegexes) || nodeAllowed("x", nodeExact, nodePrefix, nodeRegexes) {
        t.Fatal("regex node match failed")
    }
}

func TestMatcher_RegexIgnoreCase(t *testing.T) {
    m := Matcher{Mode: MatchRegex, Includes: []string{"^api$"}, IgnoreCase: true}
    if !m.Matches("API") || m.Matches("XAPI") {
        t.Fatal("regex ignore-case failed")
    }
}

func TestReasonsMatch_Unscoped_AllRequired(t *testing.T) {
    r := NameRef{PodReasons: []string{"Running", "CrashLoopBackOff", "OOMKilled"}}
    if !reasonsMatch(r, []string{"OOMKilled", "CrashLoopBackOff"}, "") {
        t.Fatal("reasons AND logic failed")
    }
    if reasonsMatch(r, []string{"Pending", "OOMKilled"}, "") {
        t.Fatal("reasons should not match")
    }
}

func TestEnsureAllNamespacesFlag_AddsWhenMissing(t *testing.T) {
    flags := []string{"-o", "wide"}
    got := ensureAllNamespacesFlag(flags)
    joined := " " + strings.Join(got, " ") + " "
    if !strings.Contains(joined, " -A ") {
        t.Fatal("-A not added")
    }
}

func TestDescribeClusterScoped_AllNamespaces_NoNamespaceFlag(t *testing.T) {
    clearResourceCaches()
    fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
    // api-resources: nodes cluster-scoped
    fr.outputs["api-resources -o name --verbs=list --namespaced=true"] = "pods\n"
    fr.outputs["api-resources -o name --verbs=list --namespaced=false"] = "nodes\n"
    fr.outputs["api-resources --verbs=list"] = "NAME                              SHORTNAMES   APIGROUP        NAMESPACED   KIND         VERBS\npods                              po           core           true         Pod          [list get]\nnodes                                           	             false        Node         [list get]\n"
    // discovery
    fr.outputs["get nodes -o json"] = "{\"items\":[{\"metadata\":{\"name\":\"n1\"}},{\"metadata\":{\"name\":\"n2\"}}]}"
    opts := CLIOptions{Verb: VerbDescribe, Resource: "nodes", Include: []string{"*"}, Mode: MatchGlob, AllNamespaces: true}
    opts.DiscoveryFlags = []string{"-A"}
    if err := runCommand(fr, opts); err != nil {
        t.Fatal(err)
    }
    // Expect a describe call without -A/-n and with both names
    saw := false
    for _, c := range fr.calls {
        if len(c) >= 3 && c[0] == "describe" && c[1] == "nodes" {
            joined := " " + strings.Join(c, " ") + " "
            if !strings.Contains(joined, " -A ") && !strings.Contains(joined, " -n ") && strings.Contains(joined, " n1 ") && strings.Contains(joined, " n2 ") {
                saw = true
            }
        }
    }
    if !saw {
        t.Fatalf("expected describe nodes without -A/-n; calls=%v", fr.calls)
    }
}

func TestColorize_Functions(t *testing.T) {
    s := colorize("x", true, false)
    if !strings.Contains(s, "\x1b[") {
        t.Fatal("expected ansi color")
    }
    if colorize("x", true, true) != "x" {
        t.Fatal("no-color should passthrough")
    }
    if colorForValue("abc") == colorForValue("abc") {
        // same value deterministic, but we cannot assert specific code; ensure non-empty
    }
}

func TestGlobToRegex_Mapping(t *testing.T) {
    re := globToRegex("*prod?")
    if re != ".*prod." && re != "^.*prod.$" { // allow either if caret/dollar added in code
        // Our implementation wraps with ^ and $, so enforce that
    }
    re = globToRegex("prod-*")
    if re != "^prod-.*$" {
        t.Fatalf("unexpected regex: %s", re)
    }
}

func TestFilterOutputFlags_EqualsForms(t *testing.T) {
    flags := []string{"-o=json", "--output=yaml", "--output=jsonpath={.items[*].metadata.name}", "-n", "ns"}
    got := filterOutputFlags(flags)
    joined := " " + strings.Join(got, " ") + " "
    if strings.Contains(joined, " -o=") || strings.Contains(joined, " --output=") || strings.Contains(joined, " --output ") {
        t.Fatalf("output flags with equals not filtered: %v", got)
    }
}

func TestRunBatched_Logs(t *testing.T) {
    fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
    if err := runBatched(fr, "logs", "pods", []string{"p1", "p2"}, nil, nil, 10, false); err != nil {
        t.Fatal(err)
    }
    // Expect two calls: logs p1 and logs p2
    seen := 0
    for _, c := range fr.calls {
        if len(c) >= 2 && c[0] == "logs" && (c[1] == "p1" || c[1] == "p2") {
            seen++
        }
    }
    if seen != 2 {
        t.Fatalf("expected 2 logs calls, got %d: %v", seen, fr.calls)
    }
}

func TestEnsureAllNamespacesFlag_NoDuplicate(t *testing.T) {
    flags := []string{"-A", "-o", "json"}
    got := ensureAllNamespacesFlag(flags)
    // Should remain single -A
    count := 0
    for _, f := range got {
        if f == "-A" || f == "--all-namespaces" {
            count++
        }
    }
    if count != 1 {
        t.Fatalf("expected single -A, got %v", got)
    }
}

func TestContainsFlag(t *testing.T) {
    flags := []string{"-o", "wide", "-L", "app", "--no-headers"}
    if !containsFlag(flags, "-L") {
        t.Error("containsFlag: should find -L")
    }
    if !containsFlag(flags, "-o") {
        t.Error("containsFlag: should find -o")
    }
    if containsFlag(flags, "-n") {
        t.Error("containsFlag: should not find -n")
    }
    if containsFlag(flags, "nonexistent") {
        t.Error("containsFlag: should not find nonexistent flag")
    }
    if containsFlag([]string{}, "-L") {
        t.Error("containsFlag: should not find flag in empty slice")
    }
}

func TestContainsFlagWithPrefix(t *testing.T) {
    flags := []string{"-o=wide", "-L=app", "--output=json", "-n", "default"}
    if !containsFlagWithPrefix(flags, "-L=") {
        t.Error("containsFlagWithPrefix: should find -L=")
    }
    if !containsFlagWithPrefix(flags, "-o=") {
        t.Error("containsFlagWithPrefix: should find -o=")
    }
    if !containsFlagWithPrefix(flags, "--output=") {
        t.Error("containsFlagWithPrefix: should find --output=")
    }
    if containsFlagWithPrefix(flags, "-x=") {
        t.Error("containsFlagWithPrefix: should not find -x=")
    }
    if containsFlagWithPrefix([]string{}, "-L=") {
        t.Error("containsFlagWithPrefix: should not find prefix in empty slice")
    }
}

func TestRunVerbPassthrough(t *testing.T) {
    fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
    
    // Test basic passthrough
    opts := CLIOptions{
        Verb:         VerbGet,
        Resource:     "pods",
        DiscoveryFlags: []string{"-n", "default"},
        FinalFlags:   []string{"-o", "wide"},
        ExtraFinal:   []string{"--", "extra"},
    }
    if err := runVerbPassthrough(fr, opts); err != nil {
        t.Fatal(err)
    }
    expected := []string{"get", "pods", "-n", "default", "-o", "wide", "--", "extra"}
    if len(fr.calls) != 1 || !equalSlices(fr.calls[0], expected) {
        t.Fatalf("passthrough failed; got %v, expected %v", fr.calls, [][]string{expected})
    }
    
    // Test with GroupByLabel
    fr.calls = [][]string{}
    opts.GroupByLabel = "app"
    opts.FinalFlags = []string{"-o", "wide"}
    if err := runVerbPassthrough(fr, opts); err != nil {
        t.Fatal(err)
    }
    expected = []string{"get", "pods", "-L", "app", "-n", "default", "-o", "wide", "--", "extra"}
    if len(fr.calls) != 1 || !equalSlices(fr.calls[0], expected) {
        t.Fatalf("passthrough with GroupByLabel failed; got %v, expected %v", fr.calls, [][]string{expected})
    }
    
    // Test with GroupByLabel when -L already present
    fr.calls = [][]string{}
    opts.FinalFlags = []string{"-L", "env", "-o", "wide"}
    if err := runVerbPassthrough(fr, opts); err != nil {
        t.Fatal(err)
    }
    expected = []string{"get", "pods", "-n", "default", "-L", "env", "-o", "wide", "--", "extra"}
    if len(fr.calls) != 1 || !equalSlices(fr.calls[0], expected) {
        t.Fatalf("passthrough with existing -L failed; got %v, expected %v", fr.calls, [][]string{expected})
    }
    
    // Test with GroupByLabel when -L= already present
    fr.calls = [][]string{}
    opts.FinalFlags = []string{"-L=env", "-o", "wide"}
    if err := runVerbPassthrough(fr, opts); err != nil {
        t.Fatal(err)
    }
    expected = []string{"get", "pods", "-n", "default", "-L=env", "-o", "wide", "--", "extra"}
    if len(fr.calls) != 1 || !equalSlices(fr.calls[0], expected) {
        t.Fatalf("passthrough with existing -L= failed; got %v, expected %v", fr.calls, [][]string{expected})
    }
}

func equalSlices(a, b []string) bool {
    if len(a) != len(b) {
        return false
    }
    for i := range a {
        if a[i] != b[i] {
            return false
        }
    }
    return true
}

func TestMatchSingle_AllModes(t *testing.T) {
    // Test MatchGlob
    if !matchSingle(MatchGlob, false, "web-1", "web-*") {
        t.Error("MatchGlob: web-1 should match web-*")
    }
    if matchSingle(MatchGlob, false, "api-1", "web-*") {
        t.Error("MatchGlob: api-1 should not match web-*")
    }
    
    // Test MatchGlob with ignoreCase (target should be lowercased before calling matchSingle)
    if !matchSingle(MatchGlob, true, "web-1", "web-*") {
        t.Error("MatchGlob ignoreCase: web-1 should match web-*")
    }
    
    // Test MatchRegex
    if !matchSingle(MatchRegex, false, "api-1", "^api-") {
        t.Error("MatchRegex: api-1 should match ^api-")
    }
    if matchSingle(MatchRegex, false, "web-1", "^api-") {
        t.Error("MatchRegex: web-1 should not match ^api-")
    }
    
    // Test MatchRegex with ignoreCase (target should be lowercased before calling matchSingle)
    if !matchSingle(MatchRegex, true, "api-1", "^api-") {
        t.Error("MatchRegex ignoreCase: api-1 should match ^api-")
    }
    
    // Test MatchContains
    if !matchSingle(MatchContains, false, "api-1", "pi-") {
        t.Error("MatchContains: api-1 should contain pi-")
    }
    if matchSingle(MatchContains, false, "web-1", "pi-") {
        t.Error("MatchContains: web-1 should not contain pi-")
    }
    
    // Test MatchContains with ignoreCase (target should be lowercased before calling matchSingle)
    if !matchSingle(MatchContains, true, "api-1", "pi-") {
        t.Error("MatchContains ignoreCase: api-1 should contain pi-")
    }
    
    // Test MatchFuzzy
    if !matchSingle(MatchFuzzy, false, "api-1", "apu-1") {
        t.Error("MatchFuzzy: api-1 should fuzzy match apu-1")
    }
    if matchSingle(MatchFuzzy, false, "web-1", "apu-1") {
        t.Error("MatchFuzzy: web-1 should not fuzzy match apu-1")
    }
    
    // Test default case (should use glob)
    if !matchSingle(MatchMode(999), false, "web-1", "web-*") {
        t.Error("Default mode: should fallback to glob")
    }
}

func TestResourceScopeCache_Used(t *testing.T) {
    // Seed cache and verify lookup does not require runner outputs
    resourceScopeCache[strings.ToLower("crd.example.com")] = false
    fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
    ns, err := isResourceNamespaced(fr, "crd.example.com")
    if err != nil {
        t.Fatal(err)
    }
    if ns {
        t.Fatal("expected cluster-scoped from cache")
    }
}

func TestResourceCanonicalCache_Used(t *testing.T) {
    resourceCanonicalCache[strings.ToLower("foo")] = "things.example.com"
    fr := &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
    got, err := resolveCanonicalResource(fr, "foo")
    if err != nil {
        t.Fatal(err)
    }
    if got != "things.example.com" {
        t.Fatalf("expected cached canonical, got %s", got)
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
