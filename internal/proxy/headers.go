package proxy

import (
	"net/http"
	"strings"
)

var hopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Proxy-Connection":    {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

func CopyHeaders(dst, src http.Header) {
	for key, values := range src {
		if isHopByHopHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
	removeConnectionTokens(dst, src.Values("Connection"))
}

func StripAuthHeaders(headers http.Header) {
	headers.Del("Authorization")
	headers.Del("X-Api-Key")
	headers.Del("X-API-Key")
}

func isHopByHopHeader(key string) bool {
	_, ok := hopByHopHeaders[http.CanonicalHeaderKey(key)]
	return ok
}

func removeConnectionTokens(headers http.Header, connectionValues []string) {
	for _, value := range connectionValues {
		for _, token := range strings.Split(value, ",") {
			headers.Del(strings.TrimSpace(token))
		}
	}
}
