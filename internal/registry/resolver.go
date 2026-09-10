// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package registry

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/zmcp/odata-mcp/internal/constants"
)

// Headers a caller may use to select the service and credentials per request.
const (
	HeaderServiceURL   = constants.HeaderODataServiceURL
	HeaderClientID     = constants.HeaderODataClientID
	HeaderClientSecret = constants.HeaderODataClientSecret
	HeaderTokenURL     = constants.HeaderODataTokenURL
	HeaderScope        = constants.HeaderODataScope
)

const bearerPrefix = "bearer "

// Resolver turns request headers into Credentials, falling back to values the
// operator configured.
type Resolver struct {
	// Defaults supply anything the caller does not send.
	Defaults Credentials

	// AllowedServiceURLs are the service URLs a caller may select. Empty means
	// callers cannot choose one, which keeps a header off outbound requests.
	AllowedServiceURLs []string

	// RequireRequestCredentials stops Defaults supplying secret material, so a
	// request carrying no credential is refused instead of inheriting one.
	RequireRequestCredentials bool
}

// Resolve reads credentials for one request.
func (rs Resolver) Resolve(headers http.Header) (Credentials, error) {
	creds := rs.Defaults

	if rs.RequireRequestCredentials {
		creds.BearerToken = ""
		creds.ClientSecret = ""
	}

	if requested := strings.TrimSpace(headers.Get(HeaderServiceURL)); requested != "" {
		if !rs.permits(requested) {
			return Credentials{}, fmt.Errorf("registry: %s is not an allowed OData service", HeaderServiceURL)
		}
		creds.ServiceURL = requested
	}

	if bearer := parseBearer(headers.Get("Authorization")); bearer != "" {
		creds.BearerToken = bearer
		creds.ClientID = ""
		creds.ClientSecret = ""

		return creds, creds.Validate()
	}

	creds.ClientID = firstNonEmpty(headers.Get(HeaderClientID), creds.ClientID)
	creds.ClientSecret = firstNonEmpty(headers.Get(HeaderClientSecret), creds.ClientSecret)

	if requested := strings.TrimSpace(headers.Get(HeaderTokenURL)); requested != "" {
		if !rs.permitsHost(requested) {
			return Credentials{}, fmt.Errorf("registry: %s is not on an allowed host", HeaderTokenURL)
		}
		creds.TokenURL = requested
	}
	creds.Scope = firstNonEmpty(headers.Get(HeaderScope), creds.Scope)

	return creds, creds.Validate()
}

// permits reports whether a caller may point the bridge at raw. Scheme and host
// are compared separately, so an allowed prefix cannot be spoofed.
func (rs Resolver) permits(raw string) bool {
	return rs.matchAllowed(raw, true)
}

// permitsHost is permits without the path check, for the token endpoint: it
// lives beside the service, not under it, but must stay on the same host so
// the bridge cannot be made to POST credentials at an arbitrary address.
func (rs Resolver) permitsHost(raw string) bool {
	return rs.matchAllowed(raw, false)
}

func (rs Resolver) matchAllowed(raw string, checkPath bool) bool {
	target, err := url.Parse(raw)
	if err != nil || target.Host == "" {
		return false
	}

	if target.Scheme != "http" && target.Scheme != "https" {
		return false
	}

	// Userinfo would let "https://allowed.example.com@evil.test/" pass a
	// prefix check, so reject it outright.
	if target.User != nil {
		return false
	}

	for _, allowed := range rs.AllowedServiceURLs {
		permitted, err := url.Parse(strings.TrimSpace(allowed))
		if err != nil || permitted.Host == "" {
			continue
		}

		if !strings.EqualFold(permitted.Scheme, target.Scheme) {
			continue
		}
		if !strings.EqualFold(permitted.Host, target.Host) {
			continue
		}
		if !checkPath || strings.HasPrefix(target.Path, permitted.Path) {
			return true
		}
	}

	return false
}

func parseBearer(header string) string {
	if len(header) <= len(bearerPrefix) || !strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return ""
	}

	return strings.TrimSpace(header[len(bearerPrefix):])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}

	return ""
}
