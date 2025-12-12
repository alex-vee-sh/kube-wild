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
	fmt.Fprintf(os.Stderr, "  kubectl wild (get|delete|describe|top) [resource] [pattern] [flags...] [-- extra]\n\n")
	fmt.Fprintf(os.Stderr, "Key flags:\n")
	fmt.Fprintf(os.Stderr, "  Matching:\n")
	fmt.Fprintf(os.Stderr, "    --regex              Use regex matching for pattern\n")
	fmt.Fprintf(os.Stderr, "    --contains           Use substring matching for pattern\n")
	fmt.Fprintf(os.Stderr, "    --fuzzy              Use fuzzy matching (handles hashed pod names)\n")
	fmt.Fprintf(os.Stderr, "    --fuzzy-distance N   Max edit distance for fuzzy matching (default: 1)\n")
	fmt.Fprintf(os.Stderr, "    --prefix/-p VAL      Match names starting with VAL\n")
	fmt.Fprintf(os.Stderr, "    --match VAL          Add include pattern (repeatable)\n")
	fmt.Fprintf(os.Stderr, "    --exclude VAL        Add exclude pattern (repeatable)\n")
	fmt.Fprintf(os.Stderr, "    --ignore-case        Case-insensitive matching\n\n")
	fmt.Fprintf(os.Stderr, "  Scope:\n")
	fmt.Fprintf(os.Stderr, "    -n, --namespace NS   Target namespace (supports wildcards like 'prod-*')\n")
	fmt.Fprintf(os.Stderr, "    -A, --all-namespaces Discover across all namespaces\n")
	fmt.Fprintf(os.Stderr, "    --ns NS              Filter to exact namespace (repeatable)\n")
	fmt.Fprintf(os.Stderr, "    --ns-prefix PFX      Filter namespaces by prefix (repeatable)\n")
	fmt.Fprintf(os.Stderr, "    --ns-regex RE        Filter namespaces by regex (repeatable)\n\n")
	fmt.Fprintf(os.Stderr, "  Labels:\n")
	fmt.Fprintf(os.Stderr, "    --label key=glob         Filter by label value glob (repeatable)\n")
	fmt.Fprintf(os.Stderr, "    --label-prefix key=pfx   Filter by label value prefix\n")
	fmt.Fprintf(os.Stderr, "    --label-contains key=sub Filter by label value substring\n")
	fmt.Fprintf(os.Stderr, "    --label-regex key=re     Filter by label value regex\n")
	fmt.Fprintf(os.Stderr, "    --label-key-regex RE     Require label key matching regex\n")
	fmt.Fprintf(os.Stderr, "    --group-by-label KEY     Add -L column and group output by label\n")
	fmt.Fprintf(os.Stderr, "    --colorize-labels        Show colored summary when grouping\n\n")
	fmt.Fprintf(os.Stderr, "  Annotations:\n")
	fmt.Fprintf(os.Stderr, "    --annotation key=glob         Filter by annotation value glob\n")
	fmt.Fprintf(os.Stderr, "    --annotation-prefix key=pfx   Filter by annotation value prefix\n")
	fmt.Fprintf(os.Stderr, "    --annotation-contains key=sub Filter by annotation value substring\n")
	fmt.Fprintf(os.Stderr, "    --annotation-regex key=re     Filter by annotation value regex\n")
	fmt.Fprintf(os.Stderr, "    --annotation-key-regex RE     Require annotation key matching regex\n\n")
	fmt.Fprintf(os.Stderr, "  Pod health:\n")
	fmt.Fprintf(os.Stderr, "    --pod-status STATUS      Filter by pod phase/status (Running, Pending, etc.)\n")
	fmt.Fprintf(os.Stderr, "    --unhealthy              Show only unhealthy pods (not clean Running/Succeeded)\n")
	fmt.Fprintf(os.Stderr, "    --older-than DURATION    Filter pods older than duration (e.g., 1h, 7d)\n")
	fmt.Fprintf(os.Stderr, "    --younger-than DURATION  Filter pods younger than duration\n")
	fmt.Fprintf(os.Stderr, "    --restarts EXPR          Filter by restart count (>N, >=N, <N, <=N, =N)\n")
	fmt.Fprintf(os.Stderr, "    --containers-not-ready   Show pods with not-ready containers\n")
	fmt.Fprintf(os.Stderr, "    --reason REASON          Filter by container reason (OOMKilled, CrashLoopBackOff)\n")
	fmt.Fprintf(os.Stderr, "    --container-name NAME    Scope reason filter to specific container\n\n")
	fmt.Fprintf(os.Stderr, "  Node filters:\n")
	fmt.Fprintf(os.Stderr, "    --node NAME          Filter pods on exact node (repeatable)\n")
	fmt.Fprintf(os.Stderr, "    --node-prefix PFX    Filter pods on nodes by prefix\n")
	fmt.Fprintf(os.Stderr, "    --node-regex RE      Filter pods on nodes by regex\n\n")
	fmt.Fprintf(os.Stderr, "  Safety (delete):\n")
	fmt.Fprintf(os.Stderr, "    --dry-run            Preview without deleting\n")
	fmt.Fprintf(os.Stderr, "    --server-dry-run     Server-side dry-run\n")
	fmt.Fprintf(os.Stderr, "    --confirm-threshold N  Block if matches > N (unless -y)\n")
	fmt.Fprintf(os.Stderr, "    --yes/-y             Skip confirmation prompt\n")
	fmt.Fprintf(os.Stderr, "    --preview [list|table]  Preview format\n")
	fmt.Fprintf(os.Stderr, "    --no-color           Disable colored output\n\n")
	fmt.Fprintf(os.Stderr, "  Other:\n")
	fmt.Fprintf(os.Stderr, "    --batch-size N       Batch size for kubectl calls (default: 200)\n")
	fmt.Fprintf(os.Stderr, "    --debug              Show debug output\n")
	fmt.Fprintf(os.Stderr, "    --version/-v         Show version\n")
	fmt.Fprintf(os.Stderr, "    --help/-h            Show this help\n\n")
	fmt.Fprintf(os.Stderr, "Examples:\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get pods 'api-*' -n default           # Glob match\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get pods --regex '^(api|web)-' -A     # Regex across all ns\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get svc -n 'prod-*'                   # Namespace wildcard\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get pods -A --label 'app=web-*'       # Label filter\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get pods -A --restarts '>0'           # Restarted pods\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get pods -A --unhealthy               # Unhealthy pods\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild top pods 'api-*' -n prod              # Resource usage\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild delete pods -p te -n default          # Delete with confirm\n")
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
	// Pre-compute node exact map for fast lookup (only if many nodes)
	var nodeExactMap map[string]bool
	if len(opts.NodeExact) > 3 {
		nodeExactMap = make(map[string]bool, len(opts.NodeExact))
		for _, n := range opts.NodeExact {
			nodeExactMap[n] = true
		}
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
	// Pre-compute duplicate detection for label filters (avoid allocation in hot path)
	labelFiltersHaveDuplicates := false
	var labelFiltersByKey map[string][]LabelFilter
	if len(labelFilters) > 1 {
		seenKeys := make(map[string]bool, len(labelFilters))
		for _, lf := range labelFilters {
			if seenKeys[lf.Key] {
				labelFiltersHaveDuplicates = true
				break
			}
			seenKeys[lf.Key] = true
		}
		if labelFiltersHaveDuplicates {
			// Pre-compute grouped filters
			labelFiltersByKey = make(map[string][]LabelFilter, len(labelFilters))
			for _, lf := range labelFilters {
				labelFiltersByKey[lf.Key] = append(labelFiltersByKey[lf.Key], lf)
			}
		}
	}
	// Pre-compute duplicate detection for annotation filters (avoid allocation in hot path)
	annotationFiltersHaveDuplicates := false
	var annotationFiltersByKey map[string][]LabelFilter
	if len(annotationFilters) > 1 {
		seenKeys := make(map[string]bool, len(annotationFilters))
		for _, af := range annotationFilters {
			if seenKeys[af.Key] {
				annotationFiltersHaveDuplicates = true
				break
			}
			seenKeys[af.Key] = true
		}
		if annotationFiltersHaveDuplicates {
			// Pre-compute grouped filters
			annotationFiltersByKey = make(map[string][]LabelFilter, len(annotationFilters))
			for _, af := range annotationFilters {
				annotationFiltersByKey[af.Key] = append(annotationFiltersByKey[af.Key], af)
			}
		}
	}
	// Pre-compute namespace exact match map for O(1) lookup (when there are many exact namespaces)
	var nsExactMap map[string]bool
	if len(opts.NsExact) > 3 {
		// Use map for 4+ exact namespaces (threshold chosen for performance)
		nsExactMap = make(map[string]bool, len(opts.NsExact))
		for _, ns := range opts.NsExact {
			nsExactMap[ns] = true
		}
	}
	matcher := Matcher{
		Mode:                            opts.Mode,
		Includes:                        opts.Include,
		Excludes:                        opts.Exclude,
		IgnoreCase:                      opts.IgnoreCase,
		IncludeRegexes:                  includeRegexes,
		ExcludeRegexes:                  excludeRegexes,
		NsExact:                         opts.NsExact,
		NsPrefix:                        opts.NsPrefix,
		NsRegex:                         nsRegexes,
		NsExactMap:                      nsExactMap,
		FuzzyMaxDistance:                opts.FuzzyMaxDistance,
		LabelFilters:                    labelFilters,
		LabelKeyRegex:                   labelKeyRegexes,
		LabelFiltersHaveDuplicates:      labelFiltersHaveDuplicates,
		LabelFiltersByKey:               labelFiltersByKey,
		AnnotationFilters:               annotationFilters,
		AnnotationKeyRegex:              annotationKeyRegexes,
		AnnotationFiltersHaveDuplicates: annotationFiltersHaveDuplicates,
		AnnotationFiltersByKey:          annotationFiltersByKey,
	}
	// Pre-allocate matched slice with estimated capacity (assume ~10% match rate for large lists)
	estimatedCapacity := len(refs) / 10
	if estimatedCapacity < 10 {
		estimatedCapacity = 10
	}
	if estimatedCapacity > len(refs) {
		estimatedCapacity = len(refs)
	}
	if estimatedCapacity == 0 {
		estimatedCapacity = 1
	}
	matched := make([]matchedRef, 0, estimatedCapacity)
	// Pre-compute if we need labels (for group-by-label or colorize)
	needsLabels := opts.GroupByLabel != "" || opts.ColorizeLabels
	for _, r := range refs {
		// Optimize filter order: check cheapest filters first for early exit
		// 1. Namespace filter (cheapest - simple string comparison)
		if !matcher.NamespaceAllowed(r.Namespace) {
			continue
		}
		// 2. Name matching (moderate cost - pattern matching)
		nameMatches := matcher.Matches(r.Name)
		if !nameMatches && opts.AllNamespaces {
			// Only compute nsname if we're doing all-namespaces matching
			// Simple concatenation is faster than strings.Builder for short strings
			nsname := r.Namespace + "/" + r.Name
			nameMatches = matcher.Matches(nsname)
		}
		if !nameMatches {
			continue
		}
		// 3. Label filters (more expensive - map lookups and pattern matching)
		if !matcher.LabelsAllowed(r.Labels) {
			continue
		}
		// 4. Annotation filters (more expensive - map lookups and pattern matching)
		if !matcher.AnnotationsAllowed(r.Annotations) {
			continue
		}
		// All basic filters passed, now check resource-specific filters
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
			if !nodeAllowedFast(r.NodeName, opts.NodeExact, nodeExactMap, opts.NodePrefix, nodeRegexes) {
				continue
			}
		}
		// Pod status filters (only when resource == pods)
		if opts.Resource == "pods" && len(opts.PodStatuses) > 0 {
			matchesAny := false
			// Pre-lowercase pod phase once for comparison
			phaseLower := ""
			if r.PodPhase != "" {
				phaseLower = strings.ToLower(r.PodPhase)
			}
			for _, s := range opts.PodStatuses {
				ls := strings.ToLower(s)
				switch ls {
				case "running", "pending", "succeeded", "failed", "unknown":
					// Phase-based match (use pre-lowercased phase)
					if phaseLower == ls {
						if ls == "running" {
							// Ensure no extra reasons beyond phase/"Running" (exclude CrashLoopBackOff, Error, etc.)
							// Optimize: check common cases first without EqualFold
							extra := false
							for _, reason := range r.PodReasons {
								// Fast path: exact match (most common)
								if reason == r.PodPhase || reason == "Running" {
									continue
								}
								// Slow path: case-insensitive match only if needed
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
					// Container reason match - optimize with fast path
					for _, reason := range r.PodReasons {
						// Fast path: exact match (most common)
						if reason == s {
							matchesAny = true
							break
						}
						// Slow path: case-insensitive match only if needed
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
			// Optimize: use direct comparison first, then EqualFold if needed
			isRunningClean := r.PodPhase == "Running" || strings.EqualFold(r.PodPhase, "Running")
			if isRunningClean {
				for _, reason := range r.PodReasons {
					// Fast path: exact match
					if reason == r.PodPhase || reason == "Running" {
						continue
					}
					// Slow path: case-insensitive match only if needed
					if strings.EqualFold(reason, r.PodPhase) || strings.EqualFold(reason, "Running") {
						continue
					}
					isRunningClean = false
					break
				}
			}
			isSucceeded := r.PodPhase == "Succeeded" || strings.EqualFold(r.PodPhase, "Succeeded")
			if isRunningClean || isSucceeded {
				continue
			}
		}
		// Only copy labels if needed (for group-by-label or colorize)
		var labelsCopy map[string]string
		if needsLabels && r.Labels != nil {
			labelsCopy = make(map[string]string, len(r.Labels))
			for k, v := range r.Labels {
				labelsCopy[k] = v
			}
		}
		matched = append(matched, matchedRef{ns: r.Namespace, name: r.Name, labels: labelsCopy})
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

// nodeAllowedFast is an optimized version that accepts a pre-computed map for exact matches
func nodeAllowedFast(node string, nodeExact []string, nodeExactMap map[string]bool, nodePrefix []string, nodeRegexes []*regexp.Regexp) bool {
	if len(nodeExact) == 0 && len(nodePrefix) == 0 && len(nodeRegexes) == 0 {
		return true
	}
	// Optimize: use map for exact matches when available (O(1) vs O(n))
	if nodeExactMap != nil {
		if nodeExactMap[node] {
			return true
		}
	} else {
		// Fallback: iterate for exact matches (when map not pre-computed)
		for _, n := range nodeExact {
			if node == n {
				return true
			}
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

// nodeAllowed is kept for backward compatibility with tests
func nodeAllowed(node string, nodeExact []string, nodePrefix []string, nodeRegexes []*regexp.Regexp) bool {
	return nodeAllowedFast(node, nodeExact, nil, nodePrefix, nodeRegexes)
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
