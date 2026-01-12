//go:build integration
// +build integration

package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getUnstuckBin() string {
	if bin := os.Getenv("UNSTUCK_BIN"); bin != "" {
		return bin
	}
	return "unstuck"
}

func runUnstuck(t *testing.T, args ...string) (string, string, error) {
	cmd := exec.Command(getUnstuckBin(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func TestDiagnoseHealthyNamespace(t *testing.T) {
	stdout, _, err := runUnstuck(t, "diagnose", "namespace", "default")
	require.NoError(t, err)
	assert.Contains(t, stdout, "Active")
	assert.Contains(t, stdout, "not in Terminating state")
}

func TestDiagnoseNonExistentNamespace(t *testing.T) {
	_, stderr, err := runUnstuck(t, "diagnose", "namespace", "does-not-exist-12345")
	require.Error(t, err)
	assert.Contains(t, stderr, "not found")
}

func TestDiagnoseOutputJSON(t *testing.T) {
	stdout, _, err := runUnstuck(t, "diagnose", "namespace", "default", "-o", "json")
	require.NoError(t, err)

	var report map[string]interface{}
	err = json.Unmarshal([]byte(stdout), &report)
	require.NoError(t, err)
	assert.Equal(t, "Active", report["status"])
	assert.Equal(t, "namespace", report["targetType"])
}

func TestVersionFlag(t *testing.T) {
	stdout, _, err := runUnstuck(t, "--version")
	require.NoError(t, err)
	assert.Contains(t, stdout, "unstuck")
}

func TestHelpFlag(t *testing.T) {
	stdout, _, err := runUnstuck(t, "--help")
	require.NoError(t, err)
	assert.Contains(t, stdout, "diagnose")
	assert.Contains(t, stdout, "Kubernetes")
}