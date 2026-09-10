// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package client

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// OData V3 services negotiating JSON light (odata=minimalmetadata) return
// annotations without the "@" prefix and with no "d" wrapper.
const jsonLightCollection = `{
  "odata.metadata": "https://example.com/svc/$metadata#People",
  "odata.count": "520",
  "value": [{"PersonCode": "0029", "FamilyName": "Morrow"}]
}`

const verboseCollection = `{
  "d": {
    "results": [{"PersonCode": "0029", "FamilyName": "Morrow"}],
    "__count": "520",
    "__next": "https://example.com/svc/People?$skiptoken=0029"
  }
}`

func parseAsMap(t *testing.T, body string) map[string]any {
	t.Helper()

	parsed, err := parseODataResponse([]byte(body), false)
	if err != nil {
		t.Fatalf("parseODataResponse() error = %v", err)
	}

	asMap, ok := parsed.(map[string]any)
	if !ok {
		t.Fatalf("parseODataResponse() = %T, want map[string]any", parsed)
	}
	return asMap
}

func TestParseV2ResponseNormalizesJSONLightCount(t *testing.T) {
	parsed := parseAsMap(t, jsonLightCollection)

	count, ok := parsed["@odata.count"]
	if !ok {
		t.Fatalf("@odata.count missing; keys = %v", keysOf(parsed))
	}
	if count != "520" {
		t.Errorf("@odata.count = %v, want %q", count, "520")
	}

	if _, ok := parsed["value"]; !ok {
		t.Error("value missing from the normalized response")
	}
	if original, ok := parsed["odata.count"]; !ok || original != "520" {
		t.Error("the original odata.count annotation should be left in place")
	}
}

func TestParseV2ResponseNormalizesJSONLightNextLink(t *testing.T) {
	body := `{"odata.nextLink": "People?$skiptoken=0029", "value": []}`

	parsed := parseAsMap(t, body)

	if got := parsed["@odata.nextLink"]; got != "People?$skiptoken=0029" {
		t.Errorf("@odata.nextLink = %v, want the JSON-light value", got)
	}
}

func TestParseV2ResponseStillHandlesVerboseWrapper(t *testing.T) {
	parsed := parseAsMap(t, verboseCollection)

	if got := parsed["@odata.count"]; got != "520" {
		t.Errorf("@odata.count = %v, want %q", got, "520")
	}
	if got := parsed["@odata.nextLink"]; got != "https://example.com/svc/People?$skiptoken=0029" {
		t.Errorf("@odata.nextLink = %v, want the __next value", got)
	}

	results, ok := parsed["value"].([]any)
	if !ok || len(results) != 1 {
		t.Errorf("value = %v, want one row unwrapped from d.results", parsed["value"])
	}
}

func TestParseV2ResponseLeavesResponsesWithoutAnnotationsAlone(t *testing.T) {
	body := `{"PersonCode": "CEZ59", "FamilyName": "Acker"}`

	parsed := parseAsMap(t, body)

	if len(parsed) != 2 {
		t.Errorf("parsed = %v, want the single entity unchanged", parsed)
	}
	if got := parsed["PersonCode"]; got != "CEZ59" {
		t.Errorf("PersonCode = %v, want %q", got, "CEZ59")
	}
	for _, key := range []string{"@odata.count", "@odata.nextLink"} {
		if _, present := parsed[key]; present {
			t.Errorf("%s was invented for a response that carried no annotation", key)
		}
	}
}

func TestParseV2ResponsePrefersAnExistingCanonicalAnnotation(t *testing.T) {
	body := `{"odata.count": "1", "@odata.count": "520", "value": []}`

	parsed := parseAsMap(t, body)

	if got := parsed["@odata.count"]; got != "520" {
		t.Errorf("@odata.count = %v, want the existing %q left untouched", got, "520")
	}
}

// The count has to survive as far as ODataResponse, which is what the count
// action reads; normalizing the annotation alone would not fix it.
func TestParseODataResponseCarriesJSONLightCountToTheModel(t *testing.T) {
	c := &ODataClient{}

	response, err := c.parseODataResponse(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(jsonLightCollection)),
	})
	if err != nil {
		t.Fatalf("parseODataResponse() error = %v", err)
	}

	if response.Count == nil {
		t.Fatal("ODataResponse.Count = nil, want the count from the JSON-light annotation")
	}
	if *response.Count != 520 {
		t.Errorf("ODataResponse.Count = %d, want 520", *response.Count)
	}
}

func TestParseODataResponseLeavesCountNilWhenNoneWasRequested(t *testing.T) {
	c := &ODataClient{}

	response, err := c.parseODataResponse(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"value":[{"PersonCode":"0029"}]}`)),
	})
	if err != nil {
		t.Fatalf("parseODataResponse() error = %v", err)
	}

	if response.Count != nil {
		t.Errorf("ODataResponse.Count = %d, want nil", *response.Count)
	}
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

func TestNonODataErrorBodiesAreCapped(t *testing.T) {
	c := &ODataClient{}
	body := []byte("<!DOCTYPE html>" + strings.Repeat("x", 5000))

	err := c.parseErrorFromBody(body, 401)
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(err.Error()) > errorBodyLimit+64 {
		t.Errorf("error is %d bytes, want the body capped near %d", len(err.Error()), errorBodyLimit)
	}
	if !strings.HasPrefix(err.Error(), "HTTP 401: <!DOCTYPE html>") {
		t.Errorf("error = %q, want the start of the body kept", err.Error()[:40])
	}
}
