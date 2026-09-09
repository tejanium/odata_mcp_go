// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package metadata

import (
	"fmt"
	"testing"
)

const versionCSDL = `<?xml version="1.0" encoding="utf-8"?>
<edmx:Edmx xmlns:edmx="%s" Version="%s">
  <edmx:DataServices %s xmlns:m="http://schemas.microsoft.com/ado/2007/08/dataservices/metadata">
    <Schema xmlns="%s" Namespace="Probe">
      <EntityType Name="Thing">
        <Key><PropertyRef Name="ID"/></Key>
        <Property Name="ID" Type="Edm.String" Nullable="false"/>
      </EntityType>
      <EntityContainer Name="Container">
        <EntitySet Name="Things" EntityType="Probe.Thing"/>
      </EntityContainer>
    </Schema>
  </edmx:DataServices>
</edmx:Edmx>`

const (
	edmxV1NS   = "http://schemas.microsoft.com/ado/2007/06/edmx"
	edmxV4NS   = "http://docs.oasis-open.org/odata/ns/edmx"
	schemaV3NS = "http://schemas.microsoft.com/ado/2008/09/edm"
	schemaV4NS = "http://docs.oasis-open.org/odata/ns/edm"
)

func TestParseMetadataReportsTheODataVersion(t *testing.T) {
	tests := []struct {
		name        string
		edmxNS      string
		edmxVersion string
		dataService string
		schemaNS    string
		want        string
	}{
		{"v2 declares 2.0", edmxV1NS, "1.0", `m:DataServiceVersion="2.0"`, schemaV3NS, "2.0"},
		{"v3 declares 3.0", edmxV1NS, "1.0", `m:DataServiceVersion="3.0"`, schemaV3NS, "3.0"},
		{"trailing semicolon is trimmed", edmxV1NS, "1.0", `m:DataServiceVersion="3.0;"`, schemaV3NS, "3.0"},
		{"v4 has no DataServiceVersion", edmxV4NS, "4.0", "", schemaV4NS, "4.0"},
		{"neither declares a version", edmxV1NS, "1.0", "", schemaV3NS, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := fmt.Sprintf(versionCSDL, tt.edmxNS, tt.edmxVersion, tt.dataService, tt.schemaNS)

			meta, err := ParseMetadata([]byte(doc), "https://svc.example.com/odata/")
			if err != nil {
				t.Fatalf("ParseMetadata() error = %v", err)
			}

			if meta.ODataVersion != tt.want {
				t.Errorf("ODataVersion = %q, want %q", meta.ODataVersion, tt.want)
			}
		})
	}
}

func TestEdmxVersionDoesNotDistinguishV2FromV3(t *testing.T) {
	v2 := fmt.Sprintf(versionCSDL, edmxV1NS, "1.0", `m:DataServiceVersion="2.0"`, schemaV3NS)
	v3 := fmt.Sprintf(versionCSDL, edmxV1NS, "1.0", `m:DataServiceVersion="3.0"`, schemaV3NS)

	parsedV2, err := ParseMetadata([]byte(v2), "https://svc.example.com/odata/")
	if err != nil {
		t.Fatalf("ParseMetadata() error = %v", err)
	}
	parsedV3, err := ParseMetadata([]byte(v3), "https://svc.example.com/odata/")
	if err != nil {
		t.Fatalf("ParseMetadata() error = %v", err)
	}

	// This is why ODataVersion exists: Version is the EDMX document version and
	// reads "1.0" for both, so it cannot drive V2-only behaviour.
	if parsedV2.Version != parsedV3.Version {
		t.Errorf("Version differs (%q vs %q); the fallback assumption no longer holds",
			parsedV2.Version, parsedV3.Version)
	}
	if parsedV2.ODataVersion == parsedV3.ODataVersion {
		t.Error("ODataVersion is identical for V2 and V3, so it cannot drive the date default")
	}
}
