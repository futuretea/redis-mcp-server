package redis

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/futuretea/redis-mcp-server/pkg/core/masking"
)

// Redis regressions for masking engine pin 15b411195f86c8de01f10b22a4ee664130242a11.
// Independent review failed that pin. These tests state the Redis output contract.
// Do not patch pkg/core/masking to satisfy them. The replacement pin owns the rule fix.

func TestInvalidUTF8ValuePatternRemovesASCIIToken(t *testing.T) {
	raw := append([]byte{0xff}, []byte("sk_live_4242")...)
	result := emitPolicy(t, `{"enabled":true,"value_patterns":["sk_live_[0-9]+"]}`, map[string]any{
		"value": binaryPair(string(raw)),
	})
	if strings.Contains(result, resultMaskedMessage) {
		if strings.Contains(result, "sk_live_4242") {
			t.Fatalf("failure still contains the token: %s", result)
		}
		return
	}
	pair := decodeData(t, result)["value"].(map[string]any)
	decoded, err := base64.StdEncoding.DecodeString(pair["base64"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if bytesContain(decoded, []byte("sk_live_4242")) || decoded[0] != 0xff {
		t.Fatalf("pin 15b4111 left sk_live_4242 in invalid UTF-8 output %x", decoded)
	}
	if utf8, ok := pair["utf8"].(string); ok && utf8 != string(decoded) {
		t.Fatalf("pair siblings disagree: %#v", pair)
	}
}

func TestInvalidUTF8ExpansionFailsClosed(t *testing.T) {
	raw := append([]byte{0xff}, strings.Repeat("x", (1<<20)-1)...)
	result := emitPolicy(t, `{"enabled":true,"values":["x"]}`, map[string]any{
		"value": binaryPair(string(raw)),
	})
	if !strings.Contains(result, resultMaskedMessage) || strings.Contains(result, strings.Repeat("x", 64)) {
		t.Fatalf("pin 15b4111 returned expanded invalid UTF-8")
	}
}

func TestZeroWidthAlternativeRejectsOrMasksToken(t *testing.T) {
	policy, err := masking.ParseJSON(`{"enabled":true,"value_patterns":["\\b|sk_live_4242"]}`)
	if err != nil {
		if err.Error() != masking.InvalidConfigMessage || strings.Contains(err.Error(), "sk_live_4242") {
			t.Fatalf("config error = %v", err)
		}
		return
	}
	toolset := NewToolset(&payloadAdapter{}, false)
	toolset.SetPolicy(policy)
	result := emit(t, toolset, "redis_string", "get", map[string]any{
		"value": binaryPair("sk_live_4242"),
	})
	if strings.Contains(result, "sk_live_4242") {
		t.Fatalf("pin 15b4111 left sk_live_4242 after a zero-width alternative: %s", result)
	}
}

func TestJSONNumberAndBoolTokensBecomeNull(t *testing.T) {
	document := `{"n":18446744073709551615,"d":1.2300,"i":411111,"ok":true,"keep":"ordinary-visible"}`
	result := emitPolicy(t, `{"enabled":true,"values":["18446744073709551615","1.2300","411111","true"]}`, map[string]any{
		"value": binaryPair(document),
	})
	if strings.Contains(result, "18446744073709551615") || strings.Contains(result, "1.2300") || strings.Contains(result, "411111") {
		t.Fatalf("pin 15b4111 left JSON numeric tokens: %s", result)
	}
	if strings.Contains(result, "ordinary-visible") == false {
		t.Fatalf("unmatched JSON string changed: %s", result)
	}
	if strings.Contains(result, resultMaskedMessage) {
		return
	}
	pair := decodeData(t, result)["value"].(map[string]any)
	text, _ := pair["utf8"].(string)
	if !strings.Contains(text, "null") || strings.Contains(text, "true") {
		t.Fatalf("JSON bool or number stayed: %s", text)
	}
}

func emitPolicy(t *testing.T, raw string, data map[string]any) string {
	t.Helper()
	toolset := NewToolset(&payloadAdapter{}, false)
	toolset.SetPolicy(maskPolicy(t, raw))
	return emit(t, toolset, "redis_string", "get", data)
}
