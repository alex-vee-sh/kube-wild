package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
	} `json:"items"`
}

func discoverNames(runner Runner, resource string, discoveryFlags []string, allNamespaces bool) ([]NameRef, error) {
	args := []string{"get", resource, "-o", "json"}
	// filter out user-provided output flags
	args = append(args, filterOutputFlags(discoveryFlags)...)
	out, errOut, err := runner.CaptureKubectl(args)
	if err != nil {
		if len(errOut) > 0 {
			return nil, fmt.Errorf(strings.TrimSpace(string(errOut)))
		}
		return nil, err
	}

	var list K8sListPartial
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("failed to parse kubectl json output: %w", err)
	}
	var refs []NameRef
	for _, it := range list.Items {
		refs = append(refs, NameRef{Namespace: it.Metadata.Namespace, Name: it.Metadata.Name})
	}
	return refs, nil
}
