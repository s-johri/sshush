package config

import (
	"fmt"
	"strconv"
	"strings"
)

// sshOptions are the ssh_config(5) keywords, in lower case (OpenSSH 10).
// Deprecated and unsupported keywords that OpenSSH still accepts (it warns,
// it does not fail) are in the list too, so sshush does not refuse a line
// that ssh reads. Host, Match and Include are not options: sshush writes
// them itself.
var sshOptions = toSet(`
addkeystoagent addressfamily batchmode bindaddress bindinterface
canonicaldomains canonicalizefallbacklocal canonicalizehostname
canonicalizemaxdots canonicalizepermittedcnames casignaturealgorithms
certificatefile channeltimeout checkhostip ciphers clearallforwardings
compression connectionattempts connecttimeout controlmaster controlpath
controlpersist dynamicforward enableescapecommandline enablesshkeysign
escapechar exitonforwardfailure fingerprinthash forkafterauthentication
forwardagent forwardx11 forwardx11timeout forwardx11trusted gatewayports
globalknownhostsfile gssapiauthentication gssapidelegatecredentials
hashknownhosts hostbasedacceptedalgorithms hostbasedauthentication
hostkeyalgorithms hostkeyalias hostname identitiesonly identityagent
identityfile ignoreunknown ipqos kbdinteractiveauthentication
kbdinteractivedevices kexalgorithms knownhostscommand localcommand
localforward loglevel logverbose macs nohostauthenticationforlocalhost
numberofpasswordprompts obscurekeystroketiming passwordauthentication
permitlocalcommand permitremoteopen pkcs11provider port
preferredauthentications proxycommand proxyjump proxyusefdpass
pubkeyacceptedalgorithms pubkeyauthentication refuseconnection rekeylimit
remotecommand remoteforward requesttty requiredrsasize revokedhostkeys
securitykeyprovider sendenv serveralivecountmax serveraliveinterval
sessiontype setenv stdinnull streamlocalbindmask streamlocalbindunlink
stricthostkeychecking syslogfacility tag tcpkeepalive tunnel tunneldevice
updatehostkeys user userknownhostsfile verifyhostkeydns versionaddendum
visualhostkey warnweakcrypto xauthlocation

challengeresponseauthentication cipher compressionlevel fallbacktorsh
gssapikeyexchange gssapiclientidentity gssapiserveridentity
gssapirenewalforceskex gssapitrustdns hostbasedkeytypes keepalive
pubkeyacceptedkeytypes protocol rhostsauthentication rhostsrsaauthentication
rsaauthentication smartcarddevice useblacklistedkeys useprivilegedport
useroaming userknownhostsfile2 globalknownhostsfile2 uselogin dsaauthentication
`)

func toSet(words string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(words) {
		out[w] = true
	}
	return out
}

// ValidateOption checks a directive name for a Host block. ssh stops at an
// unknown name ("Bad configuration option"), and then every connection
// fails, so sshush does not write one.
func ValidateOption(key string) error {
	if key == "" {
		return fmt.Errorf("option name is empty")
	}
	if strings.ContainsAny(key, " \t\r\n=#\"") {
		return fmt.Errorf("option name %q must be one word", key)
	}
	switch strings.ToLower(key) {
	case "host", "match", "include":
		return fmt.Errorf("%s cannot be set as an option of a host", key)
	}
	if !sshOptions[strings.ToLower(key)] {
		return fmt.Errorf("unknown option %q (see ssh_config(5))", key)
	}
	return nil
}

// ValidateValue checks a directive value. It must be on one line. HostName
// and User must be one word, and Port must be 1 to 65535.
func ValidateValue(key, val string) error {
	if strings.TrimSpace(val) == "" {
		return fmt.Errorf("%s needs a value", key)
	}
	if strings.ContainsAny(val, "\r\n") {
		return fmt.Errorf("%s value must be on one line", key)
	}
	switch strings.ToLower(key) {
	case "hostname", "user":
		if strings.ContainsAny(val, " \t") {
			return fmt.Errorf("%s must not contain spaces", key)
		}
	case "port":
		p, err := strconv.Atoi(val)
		if err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("port must be a number from 1 to 65535, not %q", val)
		}
	}
	return nil
}

// ValidateAlias checks the alias of a new host: one word, with no comment
// character. Wildcards are allowed (a pattern block).
func ValidateAlias(name string) error {
	if name == "" {
		return fmt.Errorf("host alias is required")
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return fmt.Errorf("host alias %q must be one word", name)
	}
	if strings.Contains(name, "#") {
		return fmt.Errorf("host alias %q must not contain #", name)
	}
	return nil
}
