// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package client

import (
	"net/http"
	"strings"
	"testing"

	"github.com/zmcp/odata-mcp/internal/constants"
)

const probeSecret = "FZA3D59owfzu933X1fu5kVRFM3Fcf3eTJdMW"

func credentialHeaders() http.Header {
	h := http.Header{}
	h.Set(constants.HeaderODataServiceURL, "https://tenant.example.com/odata/")
	h.Set(constants.HeaderODataClientID, "client-id")
	h.Set(constants.HeaderODataClientSecret, probeSecret)
	h.Set(constants.HeaderODataTokenURL, "https://tenant.example.com/OAuth/Token")
	h.Set(constants.HeaderODataScope, "read write")
	h.Set("Authorization", "Bearer a-real-token")
	h.Set("Cookie", "session=abc")
	h.Set("X-Custom-Passthrough", "keep-me")
	return h
}

func TestShouldForwardHeaderBlocksBridgeCredentials(t *testing.T) {
	blocked := []string{
		constants.HeaderODataServiceURL,
		constants.HeaderODataClientID,
		constants.HeaderODataClientSecret,
		constants.HeaderODataTokenURL,
		constants.HeaderODataScope,
	}

	for _, name := range blocked {
		t.Run(name, func(t *testing.T) {
			if shouldForwardHeader(name) {
				t.Errorf("%s is forwarded to the OData service, leaking a credential meant for the bridge", name)
			}
			if shouldForwardHeader(strings.ToUpper(name)) {
				t.Errorf("%s is forwarded when upper-cased", name)
			}
		})
	}

	if !shouldForwardHeader("X-Custom-Passthrough") {
		t.Error("other X- headers should still be forwarded")
	}
}

func TestRedactHeadersHidesEveryCredential(t *testing.T) {
	redacted := RedactHeaders(credentialHeaders())

	rendered := ""
	for name, values := range redacted {
		rendered += name + ": " + strings.Join(values, ",") + "\n"
	}

	for _, secret := range []string{probeSecret, "Bearer a-real-token", "session=abc"} {
		if strings.Contains(rendered, secret) {
			t.Errorf("RedactHeaders() leaked %q:\n%s", secret, rendered)
		}
	}

	if got := redacted.Get(constants.HeaderODataClientID); got != "[redacted]" {
		t.Errorf("client id = %q, want it redacted alongside the secret", got)
	}
	if got := redacted.Get("X-Custom-Passthrough"); got != "keep-me" {
		t.Errorf("X-Custom-Passthrough = %q, want non-credential headers left readable", got)
	}
}

func TestRedactHeadersDoesNotMutateTheOriginal(t *testing.T) {
	original := credentialHeaders()
	_ = RedactHeaders(original)

	if got := original.Get(constants.HeaderODataClientSecret); got != probeSecret {
		t.Errorf("RedactHeaders() mutated its argument: secret is now %q", got)
	}
}
