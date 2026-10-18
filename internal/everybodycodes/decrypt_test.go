package everybodycodes

import (
	"strings"
	"testing"
)

const testKey = "0123456789abcdef0123456789abcdef"

// Fixed fixtures generated independently with OpenSSL AES-256-CBC.
var testNotes = []struct {
	plain   string
	encoded string
}{
	{"First line\nSecond line: café\n", "9440069a68fcff18927655d301c4d8cb223bc0327075d19dd1b6f157c25aad17"},
	{"Part two without trailing newline", "a2d11f4488dfc2027d15b27621673550475ebb72f3cf5c6c1f761aca42468c8d4c3f081921abf7418cfc0da5735b4200"},
	{"Third part\n", "ac9927e485737228053c2717390b9d44"},
	{"", "38ae83a3edd22a71ecf296539ddf7320"},
	{"0123456789abcdef", "7dbd88272fb25937692a3b81004741755cc246053c1506c574adf2c40f5cde7e"},
}

func TestDecryptNotes(t *testing.T) {
	for _, fixture := range testNotes {
		got, err := decryptNotes(fixture.encoded, testKey)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != fixture.plain {
			t.Errorf("decoded notes = %q, want %q", got, fixture.plain)
		}
	}
}

func TestDecryptNotesRejectsInvalidData(t *testing.T) {
	for _, tt := range []struct {
		name, encoded, key, want string
	}{
		{"short key", testNotes[0].encoded, "secret", "key length"},
		{"invalid hex", "not hex", testKey, "hexadecimal"},
		{"odd hex", "a", testKey, "hexadecimal"},
		{"empty ciphertext", "", testKey, "complete AES blocks"},
		{"partial block", "00", testKey, "complete AES blocks"},
		// Flip a byte in the preceding CBC block to corrupt the final padding.
		{"bad padding length", "6cbd88272fb25937692a3b81004741755cc246053c1506c574adf2c40f5cde7e", testKey, "padding"},
		{"inconsistent padding", "7cbd88272fb25937692a3b81004741755cc246053c1506c574adf2c40f5cde7e", testKey, "padding"},
		// OpenSSL encryption of a single 0xff byte with valid PKCS#7 padding.
		{"invalid decoded text", "0829a690f19790a7f1369cdd4f761d03", testKey, "UTF-8"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decryptNotes(tt.encoded, tt.key)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}
