package main

import (
	"fmt"
	"strconv"
	"strings"
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
	if len(head) > 1 && !strings.HasPrefix(head[1], "-") {
		opts.Resource = head[1]
	} else {
		opts.Resource = "pods"
	}
	// Compute index after resource
	idxAfterRes := 1
	if len(head) > 1 && opts.Resource == head[1] && !strings.HasPrefix(head[1], "-") {
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
		case "--regex":
			opts.Mode = MatchRegex
			continue
		case "--contains":
			opts.Mode = MatchContains
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
		}

		// discovery-affecting passthrough flags we track specially
		if f == "-A" || f == "--all-namespaces" {
			opts.AllNamespaces = true
			opts.DiscoveryFlags = append(opts.DiscoveryFlags, f)
			continue
		}
		if f == "-n" || f == "--namespace" {
			opts.DiscoveryFlags = append(opts.DiscoveryFlags, f)
			if i+1 < len(flags) && !strings.HasPrefix(flags[i+1], "-") {
				opts.Namespace = flags[i+1]
				opts.DiscoveryFlags = append(opts.DiscoveryFlags, flags[i+1])
				i++
			}
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
