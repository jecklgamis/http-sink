package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

// requestTarget is the original request URI (path and query), as the client sent it.
func requestTarget(r *http.Request) string {
	if strings.HasPrefix(r.RequestURI, "/") {
		return r.RequestURI
	}
	return r.URL.RequestURI()
}

// rawPath is the request path exactly as sent (not decoded or cleaned), without the query.
func rawPath(r *http.Request) string {
	p, _, _ := strings.Cut(requestTarget(r), "?")
	return p
}

// clientIP returns the leftmost X-Forwarded-For entry (the service runs behind
// an ingress), falling back to the socket peer, with IPv4 normalisation.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		if ip := strings.TrimSpace(first); ip != "" {
			return toIPv4(ip)
		}
	}
	return toIPv4(peerAddr(r))
}

// peerAddr is the socket peer address without the port.
func peerAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func toIPv4(ip string) string {
	if ip == "::1" {
		return "127.0.0.1"
	}
	return strings.TrimPrefix(ip, "::ffff:")
}

// scheme honours X-Forwarded-Proto behind a proxy.
func scheme(r *http.Request) string {
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		first, _, _ := strings.Cut(p, ",")
		return strings.TrimSpace(first)
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// headerMap flattens request headers httpbin-style: lower-cased names,
// repeated values joined with ", " (cookies with "; "), including host.
func headerMap(r *http.Request) map[string]string {
	out := make(map[string]string, len(r.Header)+1)
	for name, vals := range r.Header {
		sep := ", "
		if strings.EqualFold(name, "Cookie") {
			sep = "; "
		}
		out[strings.ToLower(name)] = strings.Join(vals, sep)
	}
	if r.Host != "" {
		out["host"] = r.Host
	}
	return out
}

// queryArgs maps query parameters to a string, or a list for repeated keys.
func queryArgs(r *http.Request) map[string]any {
	return valuesToMap(r.URL.Query())
}

func valuesToMap(v map[string][]string) map[string]any {
	out := make(map[string]any, len(v))
	for k, vals := range v {
		if len(vals) == 1 {
			out[k] = vals[0]
		} else {
			out[k] = vals
		}
	}
	return out
}

type bodyKey struct{}

// requestBody returns the parsed JSON/form body, or nil if there was none.
func requestBody(r *http.Request) any { return r.Context().Value(bodyKey{}) }

// bodyObject returns the parsed body if it is a JSON/form object.
func bodyObject(r *http.Request) map[string]any {
	m, _ := requestBody(r).(map[string]any)
	return m
}

func withBody(r *http.Request, body any) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), bodyKey{}, body))
}

// hasBody reports whether a parsed body is worth recording.
func hasBody(body any) bool {
	switch b := body.(type) {
	case nil:
		return false
	case map[string]any:
		return len(b) > 0
	case []any:
		return len(b) > 0
	default:
		return true
	}
}

// toNumber converts a decoded value the way JavaScript's Number() would for
// the types a client can send. ok is false for missing or non-numeric values.
func toNumber(m map[string]any, key string) (float64, bool) {
	v, present := m[key]
	if !present {
		return 0, false
	}
	switch n := v.(type) {
	case nil:
		return 0, true
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0, true
		}
		f, err := parseFloat(s)
		return f, err == nil
	default:
		return 0, false
	}
}
