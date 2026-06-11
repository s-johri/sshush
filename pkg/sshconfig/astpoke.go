// astpoke.go is the single place that reaches into kevinburke/ssh_config's
// unexported fields via reflect/unsafe — the adapter between this package's
// domain logic and the library's private AST shape.
//
// Everything here is pinned to ssh_config v1.6.0: a dependency bump that
// renames or retypes one of these fields breaks the accessors. The tripwire
// test (astpoke_test.go) parses a fixture and asserts every accessor still
// binds, so an incompatible upgrade fails loudly in CI instead of corrupting
// writes. No other file in the package may import unsafe or call reflect on
// library types.
package sshconfig

import (
	"reflect"
	"unsafe"

	sshcfg "github.com/kevinburke/ssh_config"
)

// isImplicitHost reports whether h is the parser's synthetic global block (the
// implicit "Host *" that holds directives before the first Host line).
func isImplicitHost(h *sshcfg.Host) bool {
	return reflect.ValueOf(h).Elem().FieldByName("implicit").Bool()
}

// isMatchHost reports whether h came from a `Match` directive.
func isMatchHost(h *sshcfg.Host) bool {
	return reflect.ValueOf(h).Elem().FieldByName("isMatch").Bool()
}

// matchKeywordOf returns the original-case word after `Match` (e.g. "Host" or
// "all"), preserved by the parser for round-tripping.
func matchKeywordOf(h *sshcfg.Host) string {
	return reflect.ValueOf(h).Elem().FieldByName("matchKeyword").String()
}

// blockIndent returns the leading-space width of the first directive in a host
// block, so appended directives line up. Defaults to 4 spaces.
func blockIndent(host *sshcfg.Host) int {
	for _, node := range host.Nodes {
		if kv, ok := node.(*sshcfg.KV); ok {
			return int(reflect.ValueOf(kv).Elem().FieldByName("leadingSpace").Int())
		}
	}
	return 4
}

// setKVValue updates a KV's value so String() emits it. The library always
// populates the unexported rawValue from the parsed text and prefers it over
// Value, so rawValue must be cleared. Indentation, spacing, and any trailing
// comment are preserved.
func setKVValue(kv *sshcfg.KV, val string) {
	kv.Value = val
	setUnexportedString(kv, "rawValue", "")
}

// setKVIndent sets a freshly created KV's leading indentation (unexported).
func setKVIndent(kv *sshcfg.KV, spaces int) {
	v := reflect.ValueOf(kv).Elem().FieldByName("leadingSpace")
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().SetInt(int64(spaces))
}

// setUnexportedString writes an unexported string field on ptr's struct.
func setUnexportedString(ptr any, field, val string) {
	v := reflect.ValueOf(ptr).Elem().FieldByName(field)
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().SetString(val)
}
