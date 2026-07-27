package config

import "testing"

func TestProfileConfig_Validate_CredentialsBundleOnly(t *testing.T) {
	p := &ProfileConfig{Credentials: []CredentialEntry{{Bundle: "ollama"}}}
	if err := p.Validate("test"); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestProfileConfig_Validate_CredentialsAdHocOnly(t *testing.T) {
	p := &ProfileConfig{Credentials: []CredentialEntry{
		{Host: "~/.aws/credentials", Container: "/home/code/.aws/credentials"},
	}}
	if err := p.Validate("test"); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestProfileConfig_Validate_CredentialsRejectsBundleAndAdHocTogether(t *testing.T) {
	p := &ProfileConfig{Credentials: []CredentialEntry{
		{Bundle: "ollama", Host: "~/x", Container: "/x"},
	}}
	if err := p.Validate("test"); err == nil {
		t.Fatal("expected error when both bundle and host/container are set")
	}
}

func TestProfileConfig_Validate_CredentialsRejectsNeitherBundleNorAdHoc(t *testing.T) {
	p := &ProfileConfig{Credentials: []CredentialEntry{{}}}
	if err := p.Validate("test"); err == nil {
		t.Fatal("expected error when neither bundle nor host/container is set")
	}
}

func TestProfileConfig_Validate_CredentialsRejectsAdHocMissingContainer(t *testing.T) {
	p := &ProfileConfig{Credentials: []CredentialEntry{{Host: "~/x"}}}
	if err := p.Validate("test"); err == nil {
		t.Fatal("expected error when container path is missing")
	}
}

func TestProfileConfig_Validate_CredentialsRejectsAdHocMissingHost(t *testing.T) {
	p := &ProfileConfig{Credentials: []CredentialEntry{{Container: "/x"}}}
	if err := p.Validate("test"); err == nil {
		t.Fatal("expected error when host path is missing")
	}
}

func TestProfileConfig_Validate_CredentialsRejectsInvalidMode(t *testing.T) {
	p := &ProfileConfig{Credentials: []CredentialEntry{{Bundle: "ollama", Mode: "0999"}}}
	if err := p.Validate("test"); err == nil {
		t.Fatal("expected error for invalid octal mode")
	}
}

func TestProfileConfig_Validate_CredentialsAcceptsValidMode(t *testing.T) {
	p := &ProfileConfig{Credentials: []CredentialEntry{{Bundle: "ollama", Mode: "0600"}}}
	if err := p.Validate("test"); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestApplyProfile_MergesCredentials(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]ProfileConfig{
			"test": {Credentials: []CredentialEntry{{Bundle: "ollama"}}},
		},
	}
	if err := cfg.ApplyProfile("test"); err != nil {
		t.Fatalf("ApplyProfile() error = %v", err)
	}
	if len(cfg.Credentials) != 1 || cfg.Credentials[0].Bundle != "ollama" {
		t.Fatalf("Credentials not merged: %+v", cfg.Credentials)
	}
}
