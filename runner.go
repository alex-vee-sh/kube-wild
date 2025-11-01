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

func (ExecRunner) RunKubectl(args []string) error {
	cmd := exec.Command("kubectl", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (ExecRunner) CaptureKubectl(args []string) ([]byte, []byte, error) {
	cmd := exec.Command("kubectl", args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// K8sListPartial is a minimal subset for -o json parsing
type K8sListPartial struct {
	Items []struct {
		Metadata struct {
			Name              string `json:"name"`
			Namespace         string `json:"namespace"`
			CreationTimestamp string `json:"creationTimestamp"`
		} `json:"metadata"`
		Status *struct {
			Phase             string `json:"phase"`
			ContainerStatuses []struct {
				State *struct {
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
	// filter out user-provided output flags
	args = append(args, filterOutputFlags(discoveryFlags)...)
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
		if it.Status != nil {
			// include pod phase (Pending, Running, Succeeded, Failed, Unknown)
			if it.Status.Phase != "" {
				phase = it.Status.Phase
				reasons = append(reasons, it.Status.Phase)
			}
			for _, cs := range it.Status.ContainerStatuses {
				if cs.State != nil {
					if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
						reasons = append(reasons, cs.State.Waiting.Reason)
					}
					if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
						reasons = append(reasons, cs.State.Terminated.Reason)
					}
					// running state has no reason; surface as "Running" for filters
					if cs.State.Running != nil {
						reasons = append(reasons, "Running")
					}
				}
			}
		}
		refs = append(refs, NameRef{Namespace: it.Metadata.Namespace, Name: it.Metadata.Name, CreatedAt: created, PodReasons: reasons, PodPhase: phase})
	}
	return refs, nil
}
