package config

import "testing"

// The tests in this file guard T12: one bad line in ~/.ssh/config makes
// every ssh connection fail, so sshush must not write one.

func TestValidateOption(t *testing.T) {
	for _, ok := range []string{"ForwardAgent", "forwardagent", "ProxyJump", "IdentityFile",
		"SetEnv", "Tag", "PubkeyAcceptedKeyTypes", "ChallengeResponseAuthentication"} {
		if err := ValidateOption(ok); err != nil {
			t.Errorf("ValidateOption(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"ForwadAgent", "", "Host", "Match", "Include", "Forward Agent", "User\n"} {
		if err := ValidateOption(bad); err == nil {
			t.Errorf("ValidateOption(%q) = nil, want an error", bad)
		}
	}
}

func TestValidateValue(t *testing.T) {
	ok := [][2]string{
		{"Port", "22"}, {"Port", "65535"}, {"HostName", "10.0.0.5"}, {"User", "deploy"},
		{"ProxyCommand", "ssh -W %h:%p bastion"}, {"IdentityFile", "~/.ssh/id_ed25519"},
	}
	for _, kv := range ok {
		if err := ValidateValue(kv[0], kv[1]); err != nil {
			t.Errorf("ValidateValue(%q, %q) = %v", kv[0], kv[1], err)
		}
	}
	bad := [][2]string{
		{"Port", "abc"}, {"Port", "0"}, {"Port", "65536"}, {"Port", "-1"},
		{"HostName", "a b"}, {"User", "a\tb"}, {"ProxyJump", ""},
		{"ProxyCommand", "a\nHost evil"}, {"User", "u\r"},
	}
	for _, kv := range bad {
		if err := ValidateValue(kv[0], kv[1]); err == nil {
			t.Errorf("ValidateValue(%q, %q) = nil, want an error", kv[0], kv[1])
		}
	}
}

func TestValidateAlias(t *testing.T) {
	for _, ok := range []string{"prod-web", "db.internal", "*.corp"} {
		if err := ValidateAlias(ok); err != nil {
			t.Errorf("ValidateAlias(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a b", "a\tb", "a#b", "a\n"} {
		if err := ValidateAlias(bad); err == nil {
			t.Errorf("ValidateAlias(%q) = nil, want an error", bad)
		}
	}
}
