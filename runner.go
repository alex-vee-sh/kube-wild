package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Runner interface {
	RunKubectl(args []string) error
	CaptureKubectl(args []string) (stdout []byte, stderr []byte, err error)
}

type ExecRunner struct{}

func kubectlBin() string {
    if b := os.Getenv("WILD_KUBECTL"); b != "" {
        return b
    }
    if b := os.Getenv("KUBECTL"); b != "" {
        return b
    }
    return "kubectl"
}

func (ExecRunner) RunKubectl(args []string) error {
    cmd := exec.Command(kubectlBin(), args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (ExecRunner) CaptureKubectl(args []string) ([]byte, []byte, error) {
    cmd := exec.Command(kubectlBin(), args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// In-process caches (per run). Safe without locks for single-threaded CLI usage.
var resourceScopeCache = map[string]bool{}
var resourceCanonicalCache = map[string]string{}

// clearResourceCaches clears the resource caches (for testing)
func clearResourceCaches() {
	resourceScopeCache = map[string]bool{}
	resourceCanonicalCache = map[string]string{}
}

// isResourceNamespaced determines if a given resource name (e.g., "pods", "bgppeers" or
// "bgppeers.metallb.io") is namespaced by consulting `kubectl api-resources`.
// Returns true if namespaced, false if cluster-scoped. If detection fails, defaults to true.
func isResourceNamespaced(runner Runner, resource string) (bool, error) {
    if v, ok := resourceScopeCache[strings.ToLower(resource)]; ok {
        return v, nil
    }
    // Helper to check membership of resource in a list returned by api-resources -o name
    matches := func(list string, res string) bool {
        res = strings.ToLower(res)
        lines := strings.Split(strings.ToLower(list), "\n")
        for _, line := range lines {
            line = strings.TrimSpace(line)
            if line == "" {
                continue
            }
            if line == res {
                return true
            }
            if strings.HasPrefix(line, res+".") {
                return true
            }
            if strings.HasSuffix(line, "."+res) {
                return true
            }
        }
        return false
    }
    // First, check namespaced=true
    out, _, err := runner.CaptureKubectl([]string{"api-resources", "-o", "name", "--verbs=list", "--namespaced=true"})
    if err == nil && matches(string(out), resource) {
        resourceScopeCache[strings.ToLower(resource)] = true
        return true, nil
    }
    // Then, check namespaced=false
    out2, _, err2 := runner.CaptureKubectl([]string{"api-resources", "-o", "name", "--verbs=list", "--namespaced=false"})
    if err2 == nil && matches(string(out2), resource) {
        resourceScopeCache[strings.ToLower(resource)] = false
        return false, nil
    }
    // Fallback: assume namespaced if undetermined
    if err != nil {
        resourceScopeCache[strings.ToLower(resource)] = true
        return true, err
    }
    if err2 != nil {
        resourceScopeCache[strings.ToLower(resource)] = true
        return true, err2
    }
    resourceScopeCache[strings.ToLower(resource)] = true
    return true, nil
}

// resolveCanonicalResource resolves user-provided resource tokens (shortname, singular, plural,
// or group-qualified) to canonical form as printed by `kubectl api-resources -o name`, e.g.,
// "bgppeers.metallb.io". If resolution fails, returns the input unchanged.
func resolveCanonicalResource(runner Runner, resource string) (string, error) {
    lower := strings.ToLower(resource)
    if v, ok := resourceCanonicalCache[lower]; ok {
        return v, nil
    }
    // If already contains a dot, verify via -o name list and accept as-is if present
    if strings.Contains(lower, ".") {
        if out, _, err := runner.CaptureKubectl([]string{"api-resources", "-o", "name", "--verbs=list"}); err == nil {
            lines := strings.Split(strings.ToLower(string(out)), "\n")
            for _, l := range lines {
                if strings.TrimSpace(l) == lower {
                    resourceCanonicalCache[lower] = lower
                    return lower, nil
                }
            }
        }
        // fall through to attempt table-based matching
    }
    // Build index from table: NAME, SHORTNAMES, APIGROUP, NAMESPACED, KIND, VERBS
    out, _, err := runner.CaptureKubectl([]string{"api-resources", "--verbs=list"})
    if err != nil {
        // best-effort: return unchanged
        resourceCanonicalCache[lower] = lower
        return lower, err
    }
    lines := strings.Split(string(out), "\n")
    // Skip until header line containing NAME and NAMESPACED
    start := 0
    for i, ln := range lines {
        if strings.Contains(ln, "NAME") && strings.Contains(ln, "NAMESPACED") {
            start = i + 1
            break
        }
    }
    type row struct{ name, apigroup, kind string; shorts []string }
    var rows []row
    for i := start; i < len(lines); i++ {
        ln := strings.TrimSpace(lines[i])
        if ln == "" || strings.HasPrefix(ln, "NAME") {
            continue
        }
        fields := strings.Fields(ln)
        if len(fields) < 4 {
            continue
        }
        // Find index of namespaced token (true/false)
        idxNs := -1
        for j, f := range fields {
            if f == "true" || f == "false" {
                idxNs = j
                break
            }
        }
        if idxNs == -1 || idxNs+1 >= len(fields) {
            continue
        }
        name := fields[0]
        kind := fields[idxNs+1]
        apigroup := ""
        if idxNs-1 >= 1 {
            apigroup = fields[idxNs-1]
            // If apigroup looks like a shortnames CSV (no dot and not known true/false), we treat apigroup as actual group anyway; it's fine for CRDs where group present.
        }
        var shorts []string
        if idxNs-1 > 0 {
            shorts = fields[1 : idxNs-1]
        }
        rows = append(rows, row{name: strings.ToLower(name), apigroup: strings.ToLower(apigroup), kind: strings.ToLower(kind), shorts: toLowerSlice(shorts)})
    }
    // Try to match lower against canonical name, name, any shortname, or kind
    // Prefer exact group-qualified canonical first if possible
    for _, r := range rows {
        canonical := r.name
        if r.apigroup != "" && r.apigroup != "<none>" && r.apigroup != "core" {
            canonical = canonical + "." + r.apigroup
        }
        if lower == canonical || lower == r.name {
            resourceCanonicalCache[lower] = canonical
            return canonical, nil
        }
        // If input matches a shortname, return shortname as-is only for core resources
        // (to ensure kubectl/oc compatibility). For CRDs, resolve to canonical form.
        for _, s := range r.shorts {
            if lower == s {
                // Core resources: return shortname as-is (e.g., "svc" stays "svc")
                // CRDs: resolve to canonical (e.g., "bgpp" -> "bgppeers.metallb.io")
                if r.apigroup == "" || r.apigroup == "<none>" || r.apigroup == "core" {
                    resourceCanonicalCache[lower] = lower
                    return lower, nil
                } else {
                    // CRD: resolve to canonical form
                    resourceCanonicalCache[lower] = canonical
                    return canonical, nil
                }
            }
        }
        if lower == r.kind {
            resourceCanonicalCache[lower] = canonical
            return canonical, nil
        }
    }
    // No match: leave unchanged
    resourceCanonicalCache[lower] = lower
    return lower, nil
}

func toLowerSlice(ss []string) []string {
    out := make([]string, 0, len(ss))
    for _, s := range ss {
        out = append(out, strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(s, ","), ",")))
    }
    return out
}

// K8sListPartial is a minimal subset for -o json parsing
type K8sListPartial struct {
	Items []struct {
		Metadata struct {
			Name              string            `json:"name"`
			Namespace         string            `json:"namespace"`
			CreationTimestamp string            `json:"creationTimestamp"`
			Labels            map[string]string `json:"labels"`
			Annotations       map[string]string `json:"annotations"`
			OwnerReferences   []struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"ownerReferences"`
		} `json:"metadata"`
		Spec *struct {
			NodeName string `json:"nodeName"`
		} `json:"spec"`
		Status *struct {
			Phase             string `json:"phase"`
			ContainerStatuses []struct {
				Name         string `json:"name"`
				Ready        bool   `json:"ready"`
				RestartCount int    `json:"restartCount"`
				State        *struct {
					Waiting *struct {
						Reason string `json:"reason"`
					} `json:"waiting"`
					Terminated *struct {
						Reason string `json:"reason"`
					} `json:"terminated"`
					Running *struct{} `json:"running"`
				} `json:"state"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

func discoverNames(runner Runner, resource string, discoveryFlags []string) ([]NameRef, error) {
	args := []string{"get", resource, "-o", "json"}
	// Filter out user-provided output flags and drop -A/-n for cluster-scoped resources
	filtered := filterOutputFlags(discoveryFlags)
	if namespaced, err := isResourceNamespaced(runner, resource); err == nil && !namespaced {
		filtered = stripAllNamespacesFlag(stripNamespaceFlag(filtered))
	}
	args = append(args, filtered...)
	out, errOut, err := runner.CaptureKubectl(args)
	if err != nil {
		if len(errOut) > 0 {
			return nil, errors.New(strings.TrimSpace(string(errOut)))
		}
		return nil, err
	}

	var list K8sListPartial
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("failed to parse kubectl json output: %w", err)
	}
	var refs []NameRef
	for _, it := range list.Items {
		var created time.Time
		if it.Metadata.CreationTimestamp != "" {
			t, _ := time.Parse(time.RFC3339, it.Metadata.CreationTimestamp)
			created = t
		}
		var reasons []string
		var phase string
		totalRestarts := 0
		notReady := 0
		reasonsByContainer := map[string][]string{}
		if it.Status != nil {
			// include pod phase (Pending, Running, Succeeded, Failed, Unknown)
			if it.Status.Phase != "" {
				phase = it.Status.Phase
				reasons = append(reasons, it.Status.Phase)
			}
			for _, cs := range it.Status.ContainerStatuses {
				totalRestarts += cs.RestartCount
				if !cs.Ready {
					notReady++
				}
				if cs.State != nil {
					if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
						reasons = append(reasons, cs.State.Waiting.Reason)
						reasonsByContainer[cs.Name] = append(reasonsByContainer[cs.Name], cs.State.Waiting.Reason)
					}
					if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
						reasons = append(reasons, cs.State.Terminated.Reason)
						reasonsByContainer[cs.Name] = append(reasonsByContainer[cs.Name], cs.State.Terminated.Reason)
					}
					// running state has no reason; surface as "Running" for filters
					if cs.State.Running != nil {
						reasons = append(reasons, "Running")
						reasonsByContainer[cs.Name] = append(reasonsByContainer[cs.Name], "Running")
					}
				}
			}
		}
		var owners []string
		for _, o := range it.Metadata.OwnerReferences {
			if o.Kind != "" && o.Name != "" {
				owners = append(owners, o.Kind+"/"+o.Name)
			}
		}
		nodeName := ""
		if it.Spec != nil {
			nodeName = it.Spec.NodeName
		}
		refs = append(refs, NameRef{
			Namespace:          it.Metadata.Namespace,
			Name:               it.Metadata.Name,
			CreatedAt:          created,
			PodReasons:         reasons,
			PodPhase:           phase,
			Labels:             it.Metadata.Labels,
			Annotations:        it.Metadata.Annotations,
			NodeName:           nodeName,
			TotalRestarts:      totalRestarts,
			NotReadyContainers: notReady,
			ReasonsByContainer: reasonsByContainer,
			Owners:             owners,
		})
	}
	return refs, nil
}
