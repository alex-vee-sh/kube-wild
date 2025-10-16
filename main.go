package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type matchedRef struct{ ns, name string }

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild (get|delete|describe) <resource> <pattern> [flags...] [-- extra]\n\n")
	fmt.Fprintf(os.Stderr, "Examples:\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get pods 'a*' -n default\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild delete pods 'te*' -n default -y\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild describe pods --regex '^(api|web)-' -A\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get pods --prefix foo -n default   # same as 'foo*'\n")
	fmt.Fprintf(os.Stderr, "  kubectl wild get pods -p foo -n default        # short for --prefix\n")
	// logs intentionally not supported; prefer stern
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

func runCommand(runner Runner, opts CLIOptions) error {
	refs, err := discoverNames(runner, opts.Resource, opts.DiscoveryFlags, opts.AllNamespaces)
	if err != nil {
		return err
	}
	// Build list of target names (either name or ns/name when -A)
	matcher := Matcher{Mode: opts.Mode, Includes: opts.Include, Excludes: opts.Exclude, IgnoreCase: opts.IgnoreCase}
	var matched []matchedRef
	for _, r := range refs {
		nsname := r.Namespace + "/" + r.Name
		if matcher.Matches(r.Name) || matcher.Matches(nsname) {
			matched = append(matched, matchedRef{ns: r.Namespace, name: r.Name})
		}
	}
	if len(matched) == 0 {
		fmt.Fprintf(os.Stderr, "No %s matched given criteria.\n", opts.Resource)
		return nil
	}

	switch opts.Verb {
	case VerbGet:
		return runVerbPerScope(runner, "get", opts, matched)
	case VerbDescribe:
		return runVerbPerScope(runner, "describe", opts, matched)
	case VerbDelete:
		if !opts.Yes && !opts.DryRun {
			// Build preview listing as ns/name when in -A mode for clarity
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
			fmt.Printf("About to delete %d %s: %s\n", len(matched), opts.Resource, strings.Join(preview, ", "))
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
		return runVerbPerScope(runner, "delete", opts, matched)
	default:
		return fmt.Errorf("unsupported verb: %s", opts.Verb)
	}
}

func promptYesNo(prompt string) (bool, error) {
	fmt.Print(prompt)
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
		return runGetAllNamespacesSingleTable(runner, opts, matched, finalFlags)
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
			return fmt.Errorf(strings.TrimSpace(string(errOut)))
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
