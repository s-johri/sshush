package config

type KeyAlgorithm string

const (
	AlgRSA     KeyAlgorithm = "rsa"
	AlgECDSA   KeyAlgorithm = "ecdsa"
	AlgED25519 KeyAlgorithm = "ed25519"
	AlgDSA     KeyAlgorithm = "dsa"
	AlgUnknown KeyAlgorithm = "unknown"
)

type IdentityID string

// Identity represents a single ssh key pair (private and public key) that can be used for authentication.
type Identity struct {
	ID            IdentityID
	Name          string
	Path          string
	PublicKeyPath string
	Algorithm     KeyAlgorithm
	Comment       string

	// Fingerprint is the SHA256 fingerprint of the public key (e.g. "SHA256:...").
	// Computed from the key on disk and used to match against agent-loaded keys.
	Fingerprint string

	ExistsOnDisk  bool
	LoadedInAgent bool

	// AgentFingerprint is the fingerprint reported by the agent for this identity
	// once it is loaded. Equal to Fingerprint on a match; empty when not loaded.
	AgentFingerprint string
}

type HostID string

// Host represents a single ssh host configuration, which may include multiple identities (ssh keys) that can be used for authentication.
type Host struct {
	ID         HostID
	Name       string
	Hostname   string
	User       string
	Port       int
	Identities []IdentityID

	// IsPattern is true when the block's patterns are wildcards (e.g. "Host *").
	// These set defaults applied to many connections rather than a single host.
	IsPattern bool

	// Options is a map of additional ssh options that can be used for this host, such as "ProxyCommand", "ForwardAgent", etc.
	Options map[string]string
}

// SshConfigModel represents the entire ssh configuration, including all hosts and identities.
type SshConfigModel struct {
	Identities map[IdentityID]Identity
	Hosts      map[HostID]Host

	// SourceFiles is a list of all the ssh config files that were parsed to build this model, in the order they were parsed.
	// This can be used for debugging and for determining where a particular host or identity was defined.
	SourceFiles []string
}
