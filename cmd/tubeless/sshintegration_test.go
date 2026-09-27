package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSSHIntegrationBlockEscapesPercentForSSHConfig(t *testing.T) {
	block := sshIntegrationBlock()
	if !strings.Contains(block, `printf %%s "`) {
		t.Errorf("block must contain a doubled %%%%s so ssh_config's own token expansion leaves a literal %%s for the remote shell:\n%s", block)
	}
	if strings.Contains(block, `printf %s "`) {
		t.Errorf("block contains an unescaped %%s, which ssh_config's token expander rejects as an unknown token:\n%s", block)
	}
}

func TestSSHIntegrationBlockGatesOnLocalTerm(t *testing.T) {
	block := sshIntegrationBlock()
	if !strings.Contains(block, `Match exec "test \"$TERM\" = tubeless"`) {
		t.Errorf("block missing expected Match exec gate:\n%s", block)
	}
}

func TestStripManagedBlockRemovesOnlyTheMarkedRegion(t *testing.T) {
	config := "Host existing\n    HostName example.com\n"
	installed := sshIntegrationBlock() + "\n" + config

	stripped := stripManagedBlock(installed)
	if stripped != config {
		t.Errorf("stripManagedBlock = %q, want %q", stripped, config)
	}
}

func TestStripManagedBlockNoOpWithoutMarkers(t *testing.T) {
	config := "Host existing\n    HostName example.com\n"
	if got := stripManagedBlock(config); got != config {
		t.Errorf("stripManagedBlock changed config with no markers present: %q", got)
	}
}

func TestRunSSHIntegrationInstallIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	if err := os.WriteFile(configPath, []byte("Host existing\n    HostName example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := runSSHIntegrationInstall(configPath); code != 0 {
		t.Fatalf("first install exit code = %d, want 0", code)
	}
	first, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if code := runSSHIntegrationInstall(configPath); code != 0 {
		t.Fatalf("second install exit code = %d, want 0", code)
	}
	second, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(first) != string(second) {
		t.Errorf("installing twice produced different content:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if strings.Count(string(second), sshIntegrationBegin) != 1 {
		t.Errorf("expected exactly one managed block after two installs, got content:\n%s", second)
	}
	if !strings.Contains(string(second), "Host existing") {
		t.Errorf("install dropped pre-existing config content:\n%s", second)
	}
}

func TestRunSSHIntegrationRemoveRestoresOriginalConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	original := "Host existing\n    HostName example.com\n"
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := runSSHIntegrationInstall(configPath); code != 0 {
		t.Fatalf("install exit code = %d, want 0", code)
	}
	if code := runSSHIntegrationRemove(configPath); code != 0 {
		t.Fatalf("remove exit code = %d, want 0", code)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("remove left config as %q, want original %q", got, original)
	}
}

func TestRunSSHIntegrationStatusReflectsInstallState(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")

	if code := runSSHIntegrationStatus(configPath); code == 0 {
		t.Error("status reported installed before install ran")
	}

	if code := runSSHIntegrationInstall(configPath); code != 0 {
		t.Fatalf("install exit code = %d, want 0", code)
	}
	if code := runSSHIntegrationStatus(configPath); code != 0 {
		t.Error("status reported not installed after install ran")
	}
}
