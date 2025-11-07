package main

import (
    "fmt"
    "strconv"
    "strings"
    "time"
)

type Verb string

const (
	VerbGet      Verb = "get"
	VerbDelete   Verb = "delete"
	VerbDescribe Verb = "describe"
)

type MatchMode int

const (
	MatchGlob MatchMode = iota
	MatchRegex
	MatchContains
	MatchFuzzy
)

type CLIOptions struct {
	Verb       Verb
	Resource   string
	Include    []string
	Exclude    []string
	Mode       MatchMode
	IgnoreCase bool
	BatchSize  int
	Yes        bool
	DryRun     bool
	NoColor    bool
	Preview    string // "list" (default) or "table"
	// Namespace filters (applied after discovery)
	NsExact  []string
	NsPrefix []string
	NsRegex  []string
	// Safety
	ConfirmThreshold int
	ServerDryRun     bool
	Fuzzy            bool
	FuzzyMaxDistance int
	OlderThan        time.Duration
	YoungerThan      time.Duration
	PodStatuses      []string
	Unhealthy        bool
	OutputJSON       bool
	Debug            bool

	// Label filtering and grouping
	LabelFilters   []LabelFilter
	GroupByLabel   string
	ColorizeLabels bool

	// Label key presence by regex
	LabelKeyRegex []string

	// Node filters
	NodeExact  []string
	NodePrefix []string
	NodeRegex  []string

	// Pod container health
	RestartExpr        string // e.g., ">3", "<=1"
	ContainersNotReady bool
	ReasonFilters      []string
	ContainerScope     string // container name to scope reason/restart checks

	// Raw flags for discovery `kubectl get ... -o json`
	DiscoveryFlags []string
	// Raw flags for final `kubectl <verb> ...`
	FinalFlags []string

	// Whether discovery used -A (affects final name formatting)
	AllNamespaces bool
	// Namespace from -n/--namespace if provided
	Namespace string
	// Extra args after `--` are appended to FinalFlags only
	ExtraFinal []string
}

func defaultCLIOptions() CLIOptions {
	return CLIOptions{
		Mode:      MatchGlob,
		BatchSize: 200,
	}
}

// parseArgs parses plugin args. Expected minimal form:
// wild <verb> <resource> <pattern> [flags...] [-- extra]
// Supported plugin flags (stripped):
//
//	--regex, --contains, --match <p>, --exclude <p>, --ignore-case
//	--yes, -y, --dry-run, --batch-size <n>
//
// Other flags are forwarded. -A and -n/--namespace affect discovery and final.
func parseArgs(argv []string) (CLIOptions, error) {
	opts := defaultCLIOptions()
	opts.Verb = Verb(argv[0])
	switch opts.Verb {
	case VerbGet, VerbDelete, VerbDescribe:
	default:
		return opts, fmt.Errorf("unknown verb: %s", argv[0])
	}
	// split on -- to collect ExtraFinal flags
	var head []string
	var tail []string
	if idx := indexOf(argv, "--"); idx >= 0 {
		head = argv[:idx]
		tail = argv[idx+1:]
	} else {
		head = argv
	}
	// Determine resource and pattern positions with sensible defaults
	// Format: <verb> [<resource>] [<pattern>] [flags...]
	includeWasDefault := false
	// Default resource to pods if absent or next token is a flag
	// Don't normalize here - let resolveCanonicalResource handle it dynamically via kubectl api-resources
	if len(head) > 1 && !strings.HasPrefix(head[1], "-") {
		opts.Resource = head[1]
	} else {
		opts.Resource = "pods"
	}
	// Compute index after resource: if a resource token was present (even if normalized), advance by one
	idxAfterRes := 1
	if len(head) > 1 && !strings.HasPrefix(head[1], "-") {
		idxAfterRes = 2
	}
	// Pattern present?
	if len(head) > idxAfterRes && !strings.HasPrefix(head[idxAfterRes], "-") {
		opts.Include = append(opts.Include, head[idxAfterRes])
		includeWasDefault = false
		// flags start after pattern
		// flags slice defined below
	} else {
		// Default include pattern based on mode (mode may change later; adjust afterwards if needed)
		opts.Include = append(opts.Include, "*")
		includeWasDefault = true
	}
	// flags start after resource and optional pattern
	flagsStart := idxAfterRes
	if !includeWasDefault {
		flagsStart = idxAfterRes + 1
	}
	if flagsStart > len(head) {
		flagsStart = len(head)
	}
	flags := head[flagsStart:]

	// process flags, splitting plugin vs passthrough
	for i := 0; i < len(flags); i++ {
		f := flags[i]
		// plugin flags
		switch f {
		case "--debug":
			opts.Debug = true
			continue
		case "--regex":
			opts.Mode = MatchRegex
			continue
		case "--contains":
			opts.Mode = MatchContains
			continue
		case "--fuzzy":
			opts.Mode = MatchFuzzy
			opts.Fuzzy = true
			if opts.FuzzyMaxDistance == 0 {
				opts.FuzzyMaxDistance = 1
			}
			continue
		case "--fuzzy-distance":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--fuzzy-distance requires a value")
			}
			n, err := strconv.Atoi(flags[i+1])
			if err != nil || n < 1 {
				return opts, fmt.Errorf("--fuzzy-distance must be >= 1")
			}
			opts.FuzzyMaxDistance = n
			opts.Mode = MatchFuzzy
			opts.Fuzzy = true
			i++
			continue
		case "--prefix":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--prefix requires a value")
			}
			opts.Include = append(opts.Include, flags[i+1]+"*")
			i++
			continue
		case "-p":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("-p requires a value")
			}
			opts.Include = append(opts.Include, flags[i+1]+"*")
			i++
			continue
		case "--ignore-case":
			opts.IgnoreCase = true
			continue
		case "--no-color":
			opts.NoColor = true
			continue
		case "--preview":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--preview requires a value (list|table)")
			}
			opts.Preview = flags[i+1]
			i++
			continue
		case "--yes", "-y":
			opts.Yes = true
			continue
		case "--dry-run":
			opts.DryRun = true
			continue
		case "--match":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--match requires a value")
			}
			opts.Include = append(opts.Include, flags[i+1])
			i++
			continue
		case "--exclude":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--exclude requires a value")
			}
			opts.Exclude = append(opts.Exclude, flags[i+1])
			i++
			continue
		case "--batch-size":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--batch-size requires a value")
			}
			n, err := strconv.Atoi(flags[i+1])
			if err != nil || n <= 0 {
				return opts, fmt.Errorf("--batch-size must be a positive integer")
			}
			opts.BatchSize = n
			i++
			continue
		case "--ns":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--ns requires a value")
			}
			opts.NsExact = append(opts.NsExact, flags[i+1])
			i++
			continue
		case "--ns-prefix":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--ns-prefix requires a value")
			}
			opts.NsPrefix = append(opts.NsPrefix, flags[i+1])
			i++
			continue
		case "--ns-regex":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--ns-regex requires a value")
			}
			opts.NsRegex = append(opts.NsRegex, flags[i+1])
			i++
			continue
		case "--confirm-threshold":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--confirm-threshold requires a value")
			}
			n, err := strconv.Atoi(flags[i+1])
			if err != nil || n < 0 {
				return opts, fmt.Errorf("--confirm-threshold must be a non-negative integer")
			}
			opts.ConfirmThreshold = n
			i++
			continue
		case "--server-dry-run":
			opts.ServerDryRun = true
			continue
		case "--older-than":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--older-than requires a duration value (e.g., 15m, 2h, 7d)")
			}
			d, err := time.ParseDuration(flags[i+1])
			if err != nil {
				return opts, fmt.Errorf("invalid duration for --older-than")
			}
			opts.OlderThan = d
			i++
			continue
		case "--younger-than":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--younger-than requires a duration value")
			}
			d, err := time.ParseDuration(flags[i+1])
			if err != nil {
				return opts, fmt.Errorf("invalid duration for --younger-than")
			}
			opts.YoungerThan = d
			i++
			continue
		case "--pod-status":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--pod-status requires a value")
			}
			opts.PodStatuses = append(opts.PodStatuses, flags[i+1])
			i++
			continue
		case "--unhealthy", "-unhealthy":
			opts.Unhealthy = true
			continue
		case "--label":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--label requires key=pattern")
			}
			kv := flags[i+1]
			i++
			lf, err := parseLabelKV(kv, LabelGlob)
			if err != nil {
				return opts, err
			}
			opts.LabelFilters = append(opts.LabelFilters, lf)
			continue
		case "--label-prefix":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--label-prefix requires key=prefix")
			}
			kv := flags[i+1]
			i++
			lf, err := parseLabelKV(kv, LabelPrefix)
			if err != nil {
				return opts, err
			}
			opts.LabelFilters = append(opts.LabelFilters, lf)
			continue
		case "--label-contains":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--label-contains requires key=substr")
			}
			kv := flags[i+1]
			i++
			lf, err := parseLabelKV(kv, LabelContains)
			if err != nil {
				return opts, err
			}
			opts.LabelFilters = append(opts.LabelFilters, lf)
			continue
		case "--label-regex":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--label-regex requires key=regex")
			}
			kv := flags[i+1]
			i++
			lf, err := parseLabelKV(kv, LabelRegex)
			if err != nil {
				return opts, err
			}
			opts.LabelFilters = append(opts.LabelFilters, lf)
			continue
		case "--label-key-regex":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--label-key-regex requires a regex")
			}
			opts.LabelKeyRegex = append(opts.LabelKeyRegex, flags[i+1])
			i++
			continue
		case "--node":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--node requires a value")
			}
			opts.NodeExact = append(opts.NodeExact, flags[i+1])
			i++
			continue
		case "--node-prefix":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--node-prefix requires a value")
			}
			opts.NodePrefix = append(opts.NodePrefix, flags[i+1])
			i++
			continue
		case "--node-regex":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--node-regex requires a value")
			}
			opts.NodeRegex = append(opts.NodeRegex, flags[i+1])
			i++
			continue
		case "--restarts":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--restarts requires an expression like >3 or <=1")
			}
			opts.RestartExpr = flags[i+1]
			i++
			continue
		case "--containers-not-ready":
			opts.ContainersNotReady = true
			continue
		case "--reason":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--reason requires a value (e.g., OOMKilled)")
			}
			opts.ReasonFilters = append(opts.ReasonFilters, flags[i+1])
			i++
			continue
		case "--container-name":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--container-name requires a value")
			}
			opts.ContainerScope = flags[i+1]
			i++
			continue
		case "--group-by-label":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--group-by-label requires a key")
			}
			opts.GroupByLabel = flags[i+1]
			i++
			continue
		case "--colorize-labels":
			opts.ColorizeLabels = true
			continue
		case "--output":
			if i+1 >= len(flags) {
				return opts, fmt.Errorf("--output requires a value")
			}
			if flags[i+1] == "json" {
				opts.OutputJSON = true
			}
			i++
			continue
		}

		// discovery-affecting passthrough flags we track specially
		if f == "-A" || f == "--all-namespaces" {
			opts.AllNamespaces = true
			opts.DiscoveryFlags = append(opts.DiscoveryFlags, f)
			continue
		}
		if f == "-n" || f == "--namespace" {
			// Support wildcard namespace filtering via -n "xyz*" across namespaces.
			// If the provided namespace contains glob characters, treat it as a filter
			// (NsPrefix/NsRegex) and force discovery with -A. Do not forward -n to discovery.
			if i+1 < len(flags) && !strings.HasPrefix(flags[i+1], "-") {
				val := flags[i+1]
				if containsGlob(val) {
					// Simple optimization: trailing '*' with no other glob -> prefix
					if strings.HasSuffix(val, "*") && !strings.ContainsAny(val[:len(val)-1], "*?") {
						opts.NsPrefix = append(opts.NsPrefix, strings.TrimSuffix(val, "*"))
					} else {
						opts.NsRegex = append(opts.NsRegex, globToRegex(val))
					}
					opts.AllNamespaces = true
					// Ensure discovery uses -A; do not append -n pattern
					opts.DiscoveryFlags = append(opts.DiscoveryFlags, "-A")
					i++
					continue
				}
				// Exact namespace: forward to discovery and remember for final invocations
				opts.DiscoveryFlags = append(opts.DiscoveryFlags, f)
				opts.Namespace = val
				opts.DiscoveryFlags = append(opts.DiscoveryFlags, val)
				i++
				continue
			}
			// No value; just forward flag to discovery (kubectl will error and surface normally)
			opts.DiscoveryFlags = append(opts.DiscoveryFlags, f)
			continue
		}
		// equals-form namespace flags (e.g., -n=dev, --namespace=dev)
		if strings.HasPrefix(f, "-n=") || strings.HasPrefix(f, "--namespace=") {
			val := f[strings.Index(f, "=")+1:]
			if containsGlob(val) {
				if strings.HasSuffix(val, "*") && !strings.ContainsAny(val[:len(val)-1], "*?") {
					opts.NsPrefix = append(opts.NsPrefix, strings.TrimSuffix(val, "*"))
				} else {
					opts.NsRegex = append(opts.NsRegex, globToRegex(val))
				}
				opts.AllNamespaces = true
				// Ensure discovery uses -A; do not forward -n
				opts.DiscoveryFlags = append(opts.DiscoveryFlags, "-A")
				continue
			}
			// Exact namespace: forward as -n <ns> for discovery and remember for finals
			opts.DiscoveryFlags = append(opts.DiscoveryFlags, "-n", val)
			opts.Namespace = val
			continue
		}

		// output control flags for discovery should be filtered later; keep for final
		opts.DiscoveryFlags = append(opts.DiscoveryFlags, f)
		opts.FinalFlags = append(opts.FinalFlags, f)
	}
	// If pattern was defaulted earlier and mode changed, adjust default pattern accordingly
	if includeWasDefault {
		switch opts.Mode {
		case MatchGlob:
			if len(opts.Include) == 0 {
				opts.Include = []string{"*"}
			}
		case MatchRegex:
			if len(opts.Include) == 0 {
				opts.Include = []string{".*"}
			}
		case MatchContains:
			if len(opts.Include) == 0 {
				opts.Include = []string{""}
			}
		case MatchFuzzy:
			if len(opts.Include) == 0 {
				opts.Include = []string{""}
			}
		}
	}
	// If we had inserted a default include and user also provided an explicit include (e.g., -p/--prefix/--match), drop the default
	if includeWasDefault && len(opts.Include) > 1 {
		// Remove the first entry which is the default
		opts.Include = opts.Include[1:]
	}
	opts.ExtraFinal = append(opts.ExtraFinal, tail...)
	return opts, nil
}

func indexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return -1
}


// containsGlob returns true if s contains shell-style glob characters.
func containsGlob(s string) bool {
    return strings.ContainsAny(s, "*?")
}

// globToRegex converts a shell-style glob pattern to a full-string regex.
// Example: "prod-*" -> "^prod-.*$" ; "*prod?" -> ".*prod.$"
func globToRegex(glob string) string {
    var b strings.Builder
    b.WriteString("^")
    for i := 0; i < len(glob); i++ {
        c := glob[i]
        switch c {
        case '*':
            b.WriteString(".*")
        case '?':
            b.WriteString(".")
        case '.', '+', '(', ')', '|', '^', '$', '[', ']', '{', '}', '\\':
            b.WriteByte('\\')
            b.WriteByte(c)
        default:
            b.WriteByte(c)
        }
    }
    b.WriteString("$")
    return b.String()
}
