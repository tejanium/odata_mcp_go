// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package registry

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Headers a caller may use to select the service and credentials per request.
const (
	HeaderServiceURL   = "X-OData-Service-Url"
	HeaderClientID     = "X-OData-Client-Id"
	HeaderClientSecret = "X-OData-Client-Secret"
	HeaderTokenURL     = "X-OData-Token-Url"
	HeaderScope        = "X-OData-Scope"
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
}

// Resolve reads credentials for one request.
func (rs Resolver) Resolve(headers http.Header) (Credentials, error) {
	creds := rs.Defaults

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
	creds.TokenURL = firstNonEmpty(headers.Get(HeaderTokenURL), creds.TokenURL)
	creds.Scope = firstNonEmpty(headers.Get(HeaderScope), creds.Scope)

	return creds, creds.Validate()
}

// permits reports whether a caller may point the bridge at raw. Scheme and host
// are compared separately, so an allowed prefix cannot be spoofed.
func (rs Resolver) permits(raw string) bool {
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
		if strings.HasPrefix(target.Path, permitted.Path) {
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
