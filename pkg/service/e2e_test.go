//go:build e2e

// Package service e2e coverage: exercises the real wiring (disk scanner,
// config repo, ssh-agent client) against a throwaway ssh-agent, key, and config.
// Behind the `e2e` build tag so it only runs where ssh-agent/ssh-add/ssh-keygen
// exist (CI and opt-in local runs: `go test -tags e2e ./pkg/service/`).
package service_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s-johri/sshush/pkg/agent"
	"github.com/s-johri/sshush/pkg/config"
	"github.com/s-johri/sshush/pkg/keys"
	"github.com/s-johri/sshush/pkg/service"
	"github.com/s-johri/sshush/pkg/sshconfig"
)

// startAgent launches a private ssh-agent and returns its socket. The agent is
// killed when the test finishes.
func startAgent(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("ssh-agent", "-s").Output()
	if err != nil {
		t.Fatalf("start ssh-agent: %v", err)
	}
	sock := agentEnv(string(out), "SSH_AUTH_SOCK")
	pid := agentEnv(string(out), "SSH_AGENT_PID")
	if sock == "" {
		t.Fatalf("no SSH_AUTH_SOCK in ssh-agent output:\n%s", out)
	}
	t.Cleanup(func() {
		if pid != "" {
			_ = exec.Command("kill", pid).Run()
		}
	})
	return sock
}

// agentEnv pulls KEY's value out of ssh-agent's `KEY=value; export KEY;` output.
func agentEnv(out, key string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, key+"=") {
			return strings.SplitN(strings.TrimPrefix(line, key+"="), ";", 2)[0]
		}
	}
	return ""
}

// TestE2EKeyAgentConfigLifecycle drives a full round: generate a key, load it
// into a real agent, unload it, edit a config host, and delete the key.
func TestE2EKeyAgentConfigLifecycle(t *testing.T) {
	sock := startAgent(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	if err := os.WriteFile(cfgPath, []byte("Host demo\n    HostName 1.2.3.4\n    User old\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	repo := sshconfig.New(cfgPath)
	repo.SshDir = dir
	svc := service.New(keys.New(dir), repo, agent.New(sock))

	// Generate a throwaway, passphrase-less key.
	id, err := svc.GenerateKey(keys.GenerateOpts{
		Dir: dir, Name: "id_e2e", Algorithm: config.AlgED25519, Comment: "e2e",
	})
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	model, err := svc.Refresh()
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	fresh := model.Identities[id.ID]
	if !fresh.ExistsOnDisk || fresh.LoadedInAgent {
		t.Fatalf("fresh key state wrong: %+v", fresh)
	}

	// Load into the agent → fingerprint match flips LoadedInAgent.
	if err := svc.AddKeyToAgent(id.ID); err != nil {
		t.Fatalf("AddKeyToAgent: %v", err)
	}
	model, _ = svc.Refresh()
	if !model.Identities[id.ID].LoadedInAgent {
		t.Error("key not marked loaded after AddKeyToAgent")
	}

	// Unload.
	if err := svc.RemoveKeyFromAgent(id.ID); err != nil {
		t.Fatalf("RemoveKeyFromAgent: %v", err)
	}
	model, _ = svc.Refresh()
	if model.Identities[id.ID].LoadedInAgent {
		t.Error("key still loaded after RemoveKeyFromAgent")
	}

	// Config edit persists to disk (with a .bak backup).
	if err := svc.EditHost("demo", "User", "deploy"); err != nil {
		t.Fatalf("EditHost: %v", err)
	}
	raw, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(raw), "User deploy") {
		t.Errorf("config edit not persisted:\n%s", raw)
	}
	if _, err := os.Stat(cfgPath + ".bak"); err != nil {
		t.Errorf("backup not written on edit: %v", err)
	}

	// Delete removes the key files from disk.
	if err := svc.DeleteKey(id.ID); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	if _, err := os.Stat(fresh.Path); !os.IsNotExist(err) {
		t.Errorf("private key still on disk after DeleteKey: %v", err)
	}
	if _, err := os.Stat(fresh.Path + ".pub"); !os.IsNotExist(err) {
		t.Errorf("public key still on disk after DeleteKey: %v", err)
	}
}
