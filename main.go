package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

type matchedRef struct {
	ns, name string
	labels   map[string]string
}

// These are intended to be overridden at build time via -ldflags, e.g.:
// -ldflags "-X main.version=v1.0.1 -X main.commit=abc123 -X main.date=2025-11-01"
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild (get|delete|describe) [resource] [pattern] [flags...] [-- extra]\n\n")
	fmt.Fprintf(os.Stderr, "Key flags:\n")
	fmt.Fprintf(os.Stderr, "  Matching: --regex | --contains | --fuzzy [--fuzzy-distance N] | --prefix/-p VAL | --match VAL | --exclude VAL | --ignore-case\n")
	fmt.Fprintf(os.Stderr, "  Scope: -n, --namespace NS | -A, --all-namespaces | --ns NS | --ns-prefix PFX | --ns-regex RE\n")
	fmt.Fprintf(os.Stderr, "  Safety: --dry-run | --server-dry-run | --confirm-threshold N | --yes/-y | --preview [list|table] | --no-color\n")
	fmt.Fprintf(os.Stderr, "  Pod filters: --older-than DURATION | --younger-than DURATION | --pod-status STATUS\n")
	fmt.Fprintf(os.Stderr, "  Version/help: --version/-v | --help/-h\n\n")
	fmt.Fprintf(os.Stderr, "Examples:\n")
	fmt.Fprintf(os.Stderr, "  # Match by glob (default), single namespace\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get pods 'a*' -n default\n")
	fmt.Fprintf(os.Stderr, "  # Delete with preview and confirm\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild delete cm -n default -p te --preview table\n")
	fmt.Fprintf(os.Stderr, "  # Regex across all namespaces (single kubectl table)\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get pods --regex '^(api|web)-' -A\n")
	fmt.Fprintf(os.Stderr, "  # Contains mode (note: provide pattern via --match)\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get pods --contains --match pi- -n dev-x\n")
	fmt.Fprintf(os.Stderr, "  # Prefix helpers\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get pods --prefix foo -n default\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get pods -p foo -n default\n")
	fmt.Fprintf(os.Stderr, "  # Fuzzy matching with edit distance=1 (handles hashed pod names)\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get pods --fuzzy --fuzzy-distance 1 --match apu-1 -n dev-x\n")
	fmt.Fprintf(os.Stderr, "  # Namespace filters\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get pods -A --ns-prefix prod-\n")
	fmt.Fprintf(os.Stderr, "  # Namespace wildcard via -n across namespaces (adds -A implicitly)\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get svc -n 'prod-*'\n")
	fmt.Fprintf(os.Stderr, "  # Pod age/status filters\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get pods -A --younger-than 10m --pod-status Running\n")
	// logs intentionally not supported; prefer stern
	fmt.Fprintf(os.Stderr, "\nPreview & color flags (delete): --no-color, --preview table\n")
}

func main() {
	argv := os.Args[1:]
	if len(argv) == 0 {
		printUsage()
		os.Exit(2)
	}

	if argv[0] == "-h" || argv[0] == "--help" || argv[0] == "help" {
		printUsage()
		return
	}

	if argv[0] == "-v" || argv[0] == "--version" || argv[0] == "version" {
		// Print version info and exit
		v := version
		if v == "" {
			v = "dev"
		}
		if commit != "" && date != "" {
			fmt.Printf("kubectl-wild %s (%s, %s)\n", v, commit, date)
		} else if commit != "" {
			fmt.Printf("kubectl-wild %s (%s)\n", v, commit)
		} else {
			fmt.Printf("kubectl-wild %s\n", v)
		}
		return
	}

	opts, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	runner := ExecRunner{}
	if err := runCommand(runner, opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// runVerbPassthrough passes through directly to kubectl without discovery/filtering
func runVerbPassthrough(runner Runner, opts CLIOptions) error {
	args := []string{string(opts.Verb), opts.Resource}
	// Add grouping label if requested
	if opts.Verb == VerbGet && opts.GroupByLabel != "" {
		if !containsFlag(opts.FinalFlags, "-L") && !containsFlagWithPrefix(opts.FinalFlags, "-L=") {
			args = append(args, "-L", opts.GroupByLabel)
		}
		if opts.ColorizeLabels {
			// Note: can't show colored summary without discovery, but that's OK for passthrough
		}
	}
	args = append(args, opts.DiscoveryFlags...)
	args = append(args, opts.FinalFlags...)
	args = append(args, opts.ExtraFinal...)
	return runner.RunKubectl(args)
}

func runCommand(runner Runner, opts CLIOptions) error {
    // Optimization: if pattern is "*" (match all) and no filters are applied, skip discovery
    // and pass through directly to kubectl for better performance
    // Only do this for simple cases - if there are special behaviors needed, use discovery
    hasPattern := len(opts.Include) > 0 && !(len(opts.Include) == 1 && opts.Include[0] == "*")
    hasFilters := len(opts.Exclude) > 0 ||
        len(opts.NsExact) > 0 || len(opts.NsPrefix) > 0 || len(opts.NsRegex) > 0 ||
        len(opts.LabelFilters) > 0 || len(opts.LabelKeyRegex) > 0 ||
        len(opts.AnnotationFilters) > 0 || len(opts.AnnotationKeyRegex) > 0 ||
        len(opts.NodeExact) > 0 || len(opts.NodePrefix) > 0 || len(opts.NodeRegex) > 0 ||
        opts.OlderThan > 0 || opts.YoungerThan > 0 ||
        len(opts.PodStatuses) > 0 || opts.Unhealthy ||
        opts.RestartExpr != "" || opts.ContainersNotReady || len(opts.ReasonFilters) > 0
    // Only passthrough for simple get cases: no pattern, no filters, no -A, no grouping
    // This avoids complex behaviors that need discovery (single-table -A, cluster-scoped handling, etc.)
    // Also skip passthrough if resource might need resolution (no dot = might be CRD shortname/singular)
    resourceMightNeedResolution := !strings.Contains(opts.Resource, ".")
    canPassthrough := !hasPattern && !hasFilters && opts.Verb == VerbGet && 
        !opts.AllNamespaces && opts.GroupByLabel == "" && !resourceMightNeedResolution
    if canPassthrough {
        // No filtering needed - pass through directly to kubectl
        if opts.Debug {
            fmt.Fprintf(os.Stderr, "[debug] skipping discovery (no filters), passing through to kubectl\n")
        }
        return runVerbPassthrough(runner, opts)
    }
    
    // Try discovery first with the resource as-is - let kubectl/oc handle shortnames and common forms
    // Only resolve to canonical if discovery fails (likely a CRD that needs resolution)
    refs, err := discoverNames(runner, opts.Resource, opts.DiscoveryFlags)
    if err != nil {
        // Discovery failed - might be a CRD that needs canonical resolution
        // Try resolving and retry discovery
        if canon, resolveErr := resolveCanonicalResource(runner, opts.Resource); resolveErr == nil && canon != "" && canon != opts.Resource {
            if opts.Debug {
                fmt.Fprintf(os.Stderr, "[debug] discovery failed for %q, trying resolved form %q\n", opts.Resource, canon)
            }
            opts.Resource = canon
            refs, err = discoverNames(runner, opts.Resource, opts.DiscoveryFlags)
            if err != nil {
                return err
            }
        } else {
            // Resolution also failed or didn't change anything - return original error
            return err
        }
    }
	if opts.Debug {
		// quick diagnostics: show discovered items and their reasons for pods
		fmt.Fprintf(os.Stderr, "[debug] discovered %d %s\n", len(refs), opts.Resource)
        fmt.Fprintf(os.Stderr, "[debug] mode=%v includes=%v excludes=%v ignoreCase=%v nsFilters: exact=%v prefix=%v regex=%v statuses=%v\n", opts.Mode, opts.Include, opts.Exclude, opts.IgnoreCase, opts.NsExact, opts.NsPrefix, opts.NsRegex, opts.PodStatuses)
        fmt.Fprintf(os.Stderr, "[debug] flags: AllNamespaces=%v Namespace=%q DiscoveryFlags=%v FinalFlags=%v\n", opts.AllNamespaces, opts.Namespace, opts.DiscoveryFlags, opts.FinalFlags)
		if opts.Resource == "pods" {
			shown := 0
			for _, r := range refs {
				if shown >= 50 { // cap output
					fmt.Fprintln(os.Stderr, "[debug] ... (truncated)")
					break
				}
				fmt.Fprintf(os.Stderr, "[debug] %s/%s reasons=%v\n", r.Namespace, r.Name, r.PodReasons)
				shown++
			}
		}
	}
	// Build list of target names (either name or ns/name when -A)
	// Pre-compile regexes for performance
	nsRegexes := make([]*regexp.Regexp, 0, len(opts.NsRegex))
	for _, reStr := range opts.NsRegex {
		nsRegexes = append(nsRegexes, regexp.MustCompile(reStr))
	}
	labelKeyRegexes := make([]*regexp.Regexp, 0, len(opts.LabelKeyRegex))
	for _, reStr := range opts.LabelKeyRegex {
		labelKeyRegexes = append(labelKeyRegexes, regexp.MustCompile(reStr))
	}
	annotationKeyRegexes := make([]*regexp.Regexp, 0, len(opts.AnnotationKeyRegex))
	for _, reStr := range opts.AnnotationKeyRegex {
		annotationKeyRegexes = append(annotationKeyRegexes, regexp.MustCompile(reStr))
	}
	// Pre-compile node regexes
	nodeRegexes := make([]*regexp.Regexp, 0, len(opts.NodeRegex))
	for _, reStr := range opts.NodeRegex {
		nodeRegexes = append(nodeRegexes, regexp.MustCompile(reStr))
	}
	// Pre-compile include/exclude regexes when in regex mode
	var includeRegexes []*regexp.Regexp
	var excludeRegexes []*regexp.Regexp
	if opts.Mode == MatchRegex {
		includeRegexes = make([]*regexp.Regexp, 0, len(opts.Include))
		for _, pattern := range opts.Include {
			if opts.IgnoreCase {
				includeRegexes = append(includeRegexes, regexp.MustCompile("(?i)"+pattern))
			} else {
				includeRegexes = append(includeRegexes, regexp.MustCompile(pattern))
			}
		}
		excludeRegexes = make([]*regexp.Regexp, 0, len(opts.Exclude))
		for _, pattern := range opts.Exclude {
			if opts.IgnoreCase {
				excludeRegexes = append(excludeRegexes, regexp.MustCompile("(?i)"+pattern))
			} else {
				excludeRegexes = append(excludeRegexes, regexp.MustCompile(pattern))
			}
		}
	}
	// Pre-compile label/annotation regex filters
	labelFilters := make([]LabelFilter, len(opts.LabelFilters))
	for i, lf := range opts.LabelFilters {
		labelFilters[i] = lf
		if lf.Mode == LabelRegex {
			labelFilters[i].CompiledRegex = regexp.MustCompile(lf.Pattern)
		}
	}
	annotationFilters := make([]LabelFilter, len(opts.AnnotationFilters))
	for i, af := range opts.AnnotationFilters {
		annotationFilters[i] = af
		if af.Mode == LabelRegex {
			annotationFilters[i].CompiledRegex = regexp.MustCompile(af.Pattern)
		}
	}
	matcher := Matcher{
		Mode:              opts.Mode,
		Includes:          opts.Include,
		Excludes:          opts.Exclude,
		IgnoreCase:         opts.IgnoreCase,
		IncludeRegexes:     includeRegexes,
		ExcludeRegexes:     excludeRegexes,
		NsExact:            opts.NsExact,
		NsPrefix:           opts.NsPrefix,
		NsRegex:            nsRegexes,
		FuzzyMaxDistance:   opts.FuzzyMaxDistance,
		LabelFilters:       labelFilters,
		LabelKeyRegex:      labelKeyRegexes,
		AnnotationFilters:  annotationFilters,
		AnnotationKeyRegex: annotationKeyRegexes,
	}
	var matched []matchedRef
	for _, r := range refs {
		// Optimize: check name match first, only compute nsname if needed
		nameMatches := matcher.Matches(r.Name)
		if !nameMatches && opts.AllNamespaces {
			// Only compute nsname if we're doing all-namespaces matching
			nsname := r.Namespace + "/" + r.Name
			nameMatches = matcher.Matches(nsname)
		}
		if nameMatches && matcher.NamespaceAllowed(r.Namespace) && matcher.LabelsAllowed(r.Labels) && matcher.AnnotationsAllowed(r.Annotations) {
			// Age filters
			if opts.OlderThan > 0 || opts.YoungerThan > 0 {
				age := time.Since(r.CreatedAt)
				if opts.OlderThan > 0 && age < opts.OlderThan {
					continue
				}
				if opts.YoungerThan > 0 && age > opts.YoungerThan {
					continue
				}
			}
			// Node filters
			if len(opts.NodeExact) > 0 || len(opts.NodePrefix) > 0 || len(nodeRegexes) > 0 {
				if !nodeAllowed(r.NodeName, opts.NodeExact, opts.NodePrefix, nodeRegexes) {
					continue
				}
			}
			// Pod status filters (only when resource == pods)
			if opts.Resource == "pods" && len(opts.PodStatuses) > 0 {
				matchesAny := false
				for _, s := range opts.PodStatuses {
					ls := strings.ToLower(s)
					switch ls {
					case "running", "pending", "succeeded", "failed", "unknown":
						// Phase-based match
						if strings.EqualFold(r.PodPhase, s) {
							if ls == "running" {
								// Ensure no extra reasons beyond phase/"Running" (exclude CrashLoopBackOff, Error, etc.)
								extra := false
								for _, reason := range r.PodReasons {
									if strings.EqualFold(reason, r.PodPhase) || strings.EqualFold(reason, "Running") {
										continue
									}
									extra = true
									break
								}
								if !extra {
									matchesAny = true
								}
							} else {
								matchesAny = true
							}
						}
					default:
						// Container reason match
						for _, reason := range r.PodReasons {
							if strings.EqualFold(reason, s) {
								matchesAny = true
								break
							}
						}
					}
					if matchesAny {
						break
					}
				}
				if !matchesAny {
					continue
				}
			}
			// Restart expression filter
			if opts.Resource == "pods" && opts.RestartExpr != "" {
				if !compareIntExpr(r.TotalRestarts, opts.RestartExpr) {
					continue
				}
			}
			// Containers not ready
			if opts.Resource == "pods" && opts.ContainersNotReady {
				if r.NotReadyContainers == 0 {
					continue
				}
			}
			// Reason filters (optionally container-scoped)
			if opts.Resource == "pods" && len(opts.ReasonFilters) > 0 {
				if !reasonsMatch(r, opts.ReasonFilters, opts.ContainerScope) {
					continue
				}
			}
			if opts.Resource == "pods" && opts.Unhealthy {
				// unhealthy: everything that is NOT clean Running and NOT Succeeded
				isRunningClean := strings.EqualFold(r.PodPhase, "Running")
				if isRunningClean {
					for _, reason := range r.PodReasons {
						if strings.EqualFold(reason, r.PodPhase) || strings.EqualFold(reason, "Running") {
							continue
						}
						isRunningClean = false
						break
					}
				}
				isSucceeded := strings.EqualFold(r.PodPhase, "Succeeded")
				if isRunningClean || isSucceeded {
					continue
				}
			}
			matched = append(matched, matchedRef{ns: r.Namespace, name: r.Name, labels: r.Labels})
		}
	}
	if opts.Debug {
		fmt.Fprintf(os.Stderr, "[debug] matched after filters: %d\n", len(matched))
		for i, m := range matched {
			if i >= 20 {
				fmt.Fprintln(os.Stderr, "[debug] ... (truncated)")
				break
			}
			fmt.Fprintf(os.Stderr, "[debug] keep %s/%s\n", m.ns, m.name)
		}
	}
	if len(matched) == 0 {
		fmt.Fprintf(os.Stderr, "No %s matched given criteria.\n", opts.Resource)
		return nil
	}

	switch opts.Verb {
	case VerbGet:
		// If grouping by label, add -L <key> for kubectl get to keep native table output.
		// Print a colored summary ONLY when --colorize-labels is set.
		if opts.GroupByLabel != "" {
			if opts.ColorizeLabels {
				printLabelSummary(os.Stderr, opts, matched)
			}
			if !containsFlag(opts.FinalFlags, "-L") && !containsFlagWithPrefix(opts.FinalFlags, "-L=") {
				opts.FinalFlags = append([]string{"-L", opts.GroupByLabel}, opts.FinalFlags...)
			}
		}
		return runVerbPerScope(runner, "get", opts, matched)
	case VerbDescribe:
		return runVerbPerScope(runner, "describe", opts, matched)
	case VerbTop:
		return runTopVerb(runner, opts, matched)
	case VerbDelete:
		// Safety: confirm threshold BEFORE any interactive prompt
		if opts.ConfirmThreshold > 0 && len(matched) > opts.ConfirmThreshold && !opts.Yes {
			fmt.Printf("Matched %d items which exceeds confirm threshold %d. Aborting. Use -y to force.\n", len(matched), opts.ConfirmThreshold)
			return nil
		}
		if !opts.Yes && !opts.DryRun {
			previewMode := opts.Preview
			if previewMode == "" && opts.AllNamespaces {
				previewMode = "table"
			}
			if previewMode == "table" {
				if err := previewAsTable(runner, opts, matched); err != nil {
					return err
				}
			} else {
				previewAsList(opts, matched)
			}
			confirmed, err := promptYesNo("Proceed? [y/N]: ")
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Aborted.")
				return nil
			}
		}
		if opts.DryRun {
			var preview []string
			if opts.AllNamespaces {
				for _, m := range matched {
					preview = append(preview, m.ns+"/"+m.name)
				}
			} else {
				for _, m := range matched {
					preview = append(preview, m.name)
				}
			}
			fmt.Printf("[dry-run] Would delete %d %s: %s\n", len(matched), opts.Resource, strings.Join(preview, ", "))
			return nil
		}
		// Server-side dry-run
		if opts.ServerDryRun {
			opts.FinalFlags = append(opts.FinalFlags, "--dry-run=server")
		}
		return runVerbPerScope(runner, "delete", opts, matched)
	default:
		return fmt.Errorf("unsupported verb: %s", opts.Verb)
	}
}

func nodeAllowed(node string, nodeExact []string, nodePrefix []string, nodeRegexes []*regexp.Regexp) bool {
	if len(nodeExact) == 0 && len(nodePrefix) == 0 && len(nodeRegexes) == 0 {
		return true
	}
	for _, n := range nodeExact {
		if node == n {
			return true
		}
	}
	for _, p := range nodePrefix {
		if strings.HasPrefix(node, p) {
			return true
		}
	}
	for _, re := range nodeRegexes {
		if re.MatchString(node) {
			return true
		}
	}
	return false
}

func compareIntExpr(val int, expr string) bool {
	// Supports >N, >=N, <N, <=N, =N or just N (treated as =N)
	op := ""
	numStr := expr
	if strings.HasPrefix(expr, ">=") || strings.HasPrefix(expr, "<=") {
		op = expr[:2]
		numStr = expr[2:]
	} else if strings.HasPrefix(expr, ">") || strings.HasPrefix(expr, "<") || strings.HasPrefix(expr, "=") {
		op = expr[:1]
		numStr = expr[1:]
	} else {
		op = "="
	}
	n := 0
	for i := 0; i < len(numStr); i++ {
		c := numStr[i]
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + int(c-'0')
	}
	switch op {
	case ">":
		return val > n
	case ">=":
		return val >= n
	case "<":
		return val < n
	case "<=":
		return val <= n
	case "=":
		return val == n
	default:
		return false
	}
}

func reasonsMatch(r NameRef, reasons []string, container string) bool {
	if container == "" {
		for _, want := range reasons {
			matched := false
			for _, have := range r.PodReasons {
				if strings.EqualFold(have, want) {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
		return true
	}
	vals := r.ReasonsByContainer[container]
	for _, want := range reasons {
		matched := false
		for _, have := range vals {
			if strings.EqualFold(have, want) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func promptYesNo(prompt string) (bool, error) {
	// Always print confirmation prompt in bright red to draw attention
	fmt.Print("\x1b[31;1m" + prompt + "\x1b[0m")
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	t := strings.TrimSpace(text)
	if t == "y" || t == "Y" || strings.EqualFold(t, "yes") {
		return true, nil
	}
	return false, nil
}

func filterOutputFlags(flags []string) []string {
	// Drop any -o/--output flags (and their values) from the discovery call
	var filtered []string
	skipNext := false
	for i := 0; i < len(flags); i++ {
		if skipNext {
			skipNext = false
			continue
		}
		f := flags[i]
		if f == "-o" || f == "--output" {
			// skip flag and its value if present
			if i+1 < len(flags) && !strings.HasPrefix(flags[i+1], "-") {
				skipNext = true
			}
			continue
		}
		if strings.HasPrefix(f, "-o=") || strings.HasPrefix(f, "--output=") {
			continue
		}
		filtered = append(filtered, f)
	}
	return filtered
}

func stripAllNamespacesFlag(flags []string) []string {
	var out []string
	for i := 0; i < len(flags); i++ {
		f := flags[i]
		if f == "-A" || f == "--all-namespaces" {
			continue
		}
		out = append(out, f)
	}
	return out
}

func stripNamespaceFlag(flags []string) []string {
	var out []string
	skipNext := false
	for i := 0; i < len(flags); i++ {
		if skipNext {
			skipNext = false
			continue
		}
		f := flags[i]
		if f == "-n" || f == "--namespace" {
			if i+1 < len(flags) && !strings.HasPrefix(flags[i+1], "-") {
				skipNext = true
			}
			continue
		}
		out = append(out, f)
	}
	return out
}

// runTopVerb handles kubectl top command (pods/nodes)
func runTopVerb(runner Runner, opts CLIOptions, matched []matchedRef) error {
	// Map resource to kubectl top subcommand
	resourceLower := strings.ToLower(opts.Resource)
	var topSubcommand string
	switch resourceLower {
	case "pod", "pods", "po":
		topSubcommand = "pods"
	case "node", "nodes", "no":
		topSubcommand = "nodes"
	default:
		return fmt.Errorf("kubectl top only supports pods and nodes, got: %s", opts.Resource)
	}
	
	// Build targets and flags
	var targets []string
	for _, m := range matched {
		targets = append(targets, m.name)
	}
	
	finalFlags := opts.FinalFlags
	// For pods, handle namespace scoping
	// Note: kubectl top pods with -n doesn't accept multiple pod names,
	// so if we have multiple targets, we need to call without pod names
	// and let kubectl show all pods in the namespace (we've already filtered)
	if topSubcommand == "pods" {
		if opts.AllNamespaces {
			finalFlags = append([]string{"-A"}, stripNamespaceFlag(finalFlags)...)
			// With -A, don't pass pod names (kubectl shows all)
			targets = []string{}
		} else if opts.Namespace != "" {
			finalFlags = append([]string{"-n", opts.Namespace}, stripNamespaceFlag(finalFlags)...)
			// With -n and multiple pods, kubectl top doesn't accept pod names
			// If we have exactly one pod, we can pass it; otherwise show all in namespace
			if len(targets) != 1 {
				targets = []string{}
			}
		}
	}
	// For nodes, remove any namespace flags (cluster-scoped)
	if topSubcommand == "nodes" {
		finalFlags = stripAllNamespacesFlag(stripNamespaceFlag(finalFlags))
		// kubectl top nodes can accept multiple node names
	}
	
	// Call kubectl top <subcommand> <targets...>
	// For oc, use "adm top" instead of "top"
	bin := os.Getenv("WILD_KUBECTL")
	if bin == "" {
		bin = os.Getenv("KUBECTL")
	}
	if bin == "" {
		bin = "kubectl"
	}
	var args []string
	if bin == "oc" {
		// oc uses "adm top" instead of "top"
		args = []string{"adm", "top", topSubcommand}
	} else {
		args = []string{"top", topSubcommand}
	}
	args = append(args, targets...)
	args = append(args, finalFlags...)
	args = append(args, opts.ExtraFinal...)
	return runner.RunKubectl(args)
}

func runVerbPerScope(runner Runner, verb string, opts CLIOptions, matched []matchedRef) error {
	if !opts.AllNamespaces {
		// Build targets as names only, ensure -n <ns> propagated
		finalFlags := opts.FinalFlags
		if opts.Namespace != "" {
			finalFlags = append([]string{"-n", opts.Namespace}, stripNamespaceFlag(finalFlags)...)
		}
		var targets []string
		for _, m := range matched {
			targets = append(targets, m.name)
		}
		return runBatched(runner, verb, opts.Resource, targets, finalFlags, opts.ExtraFinal, opts.BatchSize, false)
	}
	// All-namespaces
    finalFlags := stripAllNamespacesFlag(stripNamespaceFlag(opts.FinalFlags))
    if verb == "get" {
        // Prefer a single kubectl call with ns/name targets and -A so kubectl prints the NAMESPACE column
        return runGetAcrossNamespaces(runner, opts, matched)
    }
    // For non-get verbs, detect cluster-scoped and avoid per-namespace iteration
    if namespaced, err := isResourceNamespaced(runner, opts.Resource); err == nil && !namespaced {
        var names []string
        for _, m := range matched {
            names = append(names, m.name)
        }
        return runBatched(runner, verb, opts.Resource, names, finalFlags, opts.ExtraFinal, opts.BatchSize, false)
    }
	nsToNames := map[string][]string{}
	for _, m := range matched {
		nsToNames[m.ns] = append(nsToNames[m.ns], m.name)
	}
	headerPrinted := false
	for ns, names := range nsToNames {
		flagsForNs := append([]string{"-n", ns}, finalFlags...)
		if err := runBatched(runner, verb, opts.Resource, names, flagsForNs, opts.ExtraFinal, opts.BatchSize, headerPrinted); err != nil {
			return err
		}
		if verb == "get" {
			headerPrinted = true
		}
	}
	return nil
}

func runBatched(runner Runner, verb string, resource string, targets []string, finalFlags []string, extra []string, batchSize int, suppressFirstHeader bool) error {
	// Avoid infinite loops when batchSize is unset/zero or negative
	if batchSize <= 0 {
		batchSize = len(targets)
		if batchSize == 0 {
			return nil
		}
	}
	for i := 0; i < len(targets); i += batchSize {
		j := i + batchSize
		if j > len(targets) {
			j = len(targets)
		}
		batch := targets[i:j]
		// Special-case logs: kubectl logs expects a single pod per invocation
		if verb == "logs" {
			for _, name := range batch {
				args := []string{verb, name}
				args = append(args, finalFlags...)
				args = append(args, extra...)
				if err := runner.RunKubectl(args); err != nil {
					return err
				}
			}
			continue
		}
		args := []string{verb, resource}
		args = append(args, batch...)
		// For 'get' only, suppress headers on subsequent batches so output looks like one table
		batchFlags := finalFlags
		if verb == "get" && (i > 0 || suppressFirstHeader) {
			batchFlags = append(batchFlags, "--no-headers=true")
		}
		args = append(args, batchFlags...)
		args = append(args, extra...)
		if err := runner.RunKubectl(args); err != nil {
			return err
		}
	}
	return nil
}

func ensureAllNamespacesFlag(flags []string) []string {
	for _, f := range flags {
		if f == "-A" || f == "--all-namespaces" {
			return flags
		}
	}
	return append(flags, "-A")
}

// Run a single or batched kubectl get across namespaces using ns/name targets with -A,
// so kubectl includes the NAMESPACE column.
func runGetAcrossNamespaces(runner Runner, opts CLIOptions, matched []matchedRef) error {
    // For cluster-scoped resources, avoid -A and fall back to normal batched get
    if namespaced, err := isResourceNamespaced(runner, opts.Resource); err == nil && !namespaced {
        finalFlags := stripAllNamespacesFlag(stripNamespaceFlag(opts.FinalFlags))
        var names []string
        for _, m := range matched {
            names = append(names, m.name)
        }
        return runBatched(runner, "get", opts.Resource, names, finalFlags, opts.ExtraFinal, opts.BatchSize, false)
    }
    // Use filtered List approach to let kubectl render a single table with NAMESPACE
    finalFlags := stripAllNamespacesFlag(stripNamespaceFlag(opts.FinalFlags))
    finalFlags = ensureAllNamespacesFlag(finalFlags)
    return runGetAllNamespacesSingleTable(runner, opts, matched, finalFlags)
}

// runGetAllNamespacesSingleTable combines results into a single kubectl table by
// filtering a JSON list and invoking kubectl once with -f.
func runGetAllNamespacesSingleTable(runner Runner, opts CLIOptions, matched []matchedRef, finalFlags []string) error {
	// Build keep set keyed by ns -> name
	keep := map[string]map[string]bool{}
	for _, m := range matched {
		if keep[m.ns] == nil {
			keep[m.ns] = map[string]bool{}
		}
		keep[m.ns][m.name] = true
	}
	// Get all as JSON
	args := []string{"get", opts.Resource, "-A", "-o", "json"}
	out, errOut, err := runner.CaptureKubectl(args)
	if err != nil {
		if len(errOut) > 0 {
			return errors.New(strings.TrimSpace(string(errOut)))
		}
		return err
	}
	type metaOnly struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
	}
	type listRaw struct {
		Items []json.RawMessage `json:"items"`
	}
	var lr listRaw
	if err := json.Unmarshal(out, &lr); err != nil {
		return fmt.Errorf("failed to parse kubectl json output: %w", err)
	}
	var filtered []json.RawMessage
	for _, item := range lr.Items {
		var mo metaOnly
		if err := json.Unmarshal(item, &mo); err != nil {
			continue
		}
		if keep[mo.Metadata.Namespace][mo.Metadata.Name] {
			filtered = append(filtered, item)
		}
	}
	list := struct {
		APIVersion string            `json:"apiVersion"`
		Kind       string            `json:"kind"`
		Items      []json.RawMessage `json:"items"`
	}{APIVersion: "v1", Kind: "List", Items: filtered}
	payload, err := json.Marshal(list)
	if err != nil {
		return err
	}
	// write to temp file (simpler than extending Runner for stdin)
	tmp, err := os.CreateTemp("", "kubectl-wild-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	callArgs := []string{"get", "-f", tmp.Name()}
	callArgs = append(callArgs, finalFlags...)
	callArgs = append(callArgs, opts.ExtraFinal...)
	return runner.RunKubectl(callArgs)
}

func colorize(s string, red bool, noColor bool) string {
	if noColor {
		return s
	}
	if red {
		return "\x1b[31;1m" + s + "\x1b[0m"
	}
	return s
}

func containsFlag(flags []string, flag string) bool {
	for i := 0; i < len(flags); i++ {
		if flags[i] == flag {
			return true
		}
	}
	return false
}

func containsFlagWithPrefix(flags []string, prefix string) bool {
	for _, f := range flags {
		if strings.HasPrefix(f, prefix) {
			return true
		}
	}
	return false
}

func colorForValue(val string) string {
	// deterministic color from hash of value
	colors := []string{"31", "32", "33", "34", "35", "36"} // red, green, yellow, blue, magenta, cyan
	h := 0
	for i := 0; i < len(val); i++ {
		h = int(val[i]) + (h << 6) + (h << 16) - h
	}
	idx := (h & 0x7fffffff) % len(colors)
	return "\x1b[" + colors[idx] + ";1m"
}

func printLabelSummary(w *os.File, opts CLIOptions, matched []matchedRef) {
	key := opts.GroupByLabel
	groups := map[string]int{}
	for _, m := range matched {
		if m.labels == nil {
			continue
		}
		val := m.labels[key]
		groups[val] = groups[val] + 1
	}
	// Print summary to stderr so table output remains clean when piped
	fmt.Fprintf(w, "Grouping by label %s:\n", key)
	for val, count := range groups {
		labelText := val
		if labelText == "" {
			labelText = "(none)"
		}
		if opts.ColorizeLabels && !opts.NoColor {
			c := colorForValue(labelText)
			fmt.Fprintf(w, "%s%s\x1b[0m → %d\n", c, labelText, count)
		} else {
			fmt.Fprintf(w, "%s → %d\n", labelText, count)
		}
	}
	fmt.Fprintf(w, "Added -L %s to kubectl output.\n", key)
}

func previewAsList(opts CLIOptions, matched []matchedRef) {
	// Columnar list: single-ns => NAME; all-ns => NAMESPACE\tRESOURCE/NAME (bright red)
	fmt.Printf("About to delete %d %s:\n", len(matched), opts.Resource)
	for _, m := range matched {
		ns := m.ns
		if ns == "" {
			ns = opts.Namespace
		}
		if ns == "" {
			ns = "(cluster-scope)"
		}
		var entry string
		if opts.AllNamespaces {
			entry = fmt.Sprintf("%s\t%s/%s", ns, opts.Resource, m.name)
		} else {
			entry = m.name
		}
		fmt.Println(colorize(entry, true, opts.NoColor))
	}
}

func previewAsTable(runner Runner, opts CLIOptions, matched []matchedRef) error {
	// Align with kubectl: when -A, use ns/name targets so NAMESPACE column is shown
	if opts.AllNamespaces {
		return runGetAcrossNamespaces(runner, opts, matched)
	}
	// Single-namespace table preview: get <resource> <names...> -n <ns>
	finalFlags := opts.FinalFlags
	if opts.Namespace != "" {
		finalFlags = append([]string{"-n", opts.Namespace}, stripNamespaceFlag(finalFlags)...)
	}
	var names []string
	for _, m := range matched {
		names = append(names, m.name)
	}
	return runBatched(runner, "get", opts.Resource, names, finalFlags, opts.ExtraFinal, opts.BatchSize, false)
}
