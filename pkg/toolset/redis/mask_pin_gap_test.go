package redis

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/futuretea/redis-mcp-server/pkg/core/masking"
)

// Redis regressions for the corrected masking engine pin
// bea65e8ab8ae5070b8ae17ab0e5c6c05bdccfd39. They run through the Redis emit path.

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

func TestNameAnchorRejectsBreakoutAndMatchesWholeNames(t *testing.T) {
	secret := "synthetic-token-value"
	_, err := masking.ParseJSON(`{"enabled":true,"values":["` + secret + `"],"name_patterns":["a)|(b"]}`)
	if err == nil || err.Error() != masking.InvalidConfigMessage || strings.Contains(err.Error(), secret) {
		t.Fatalf("breakout config error = %v", err)
	}
	whole := emitPolicy(t, `{"enabled":true,"name_patterns":["(?:a)|(b)"]}`, map[string]any{
		"values_by_key": map[string]any{
			"a":  binaryPair(secret),
			"ab": binaryPair("keep-visible"),
		},
	})
	if strings.Contains(whole, secret) || strings.Contains(whole, `"a":`) {
		t.Fatalf("whole name a leaked: %s", whole)
	}
	if !strings.Contains(whole, "keep-visible") || !strings.Contains(whole, `"ab"`) {
		t.Fatalf("longer name changed: %s", whole)
	}
	other := emitPolicy(t, `{"enabled":true,"name_patterns":["(?:a)|(b)"]}`, map[string]any{
		"values_by_key": map[string]any{"b": binaryPair("keep-b"), "aSECRET": binaryPair("keep-secret")},
	})
	if strings.Contains(other, `"b":`) || strings.Contains(other, "keep-b") || !strings.Contains(other, "aSECRET") || !strings.Contains(other, "keep-secret") {
		t.Fatalf("alternation result = %s", other)
	}
}

func TestOversizedBase64FailsBeforeDecode(t *testing.T) {
	encoded := strings.Repeat("A", (1<<20)+1)
	result := emitPolicy(t, `{"enabled":true,"values":["synthetic-token-value"]}`, map[string]any{
		"value": map[string]any{"utf8": "synthetic-token-value", "base64": encoded},
	})
	if !strings.Contains(result, resultMaskedMessage) || strings.Contains(result, "synthetic-token-value") || strings.Contains(result, strings.Repeat("A", 64)) {
		t.Fatal("oversized base64 pair was returned")
	}
}

func emitPolicy(t *testing.T, raw string, data map[string]any) string {
	t.Helper()
	toolset := NewToolset(&payloadAdapter{}, false)
	toolset.SetPolicy(maskPolicy(t, raw))
	return emit(t, toolset, "redis_string", "get", data)
}
