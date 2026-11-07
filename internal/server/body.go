package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// DefaultMaxBodyBytes caps request bodies that are parsed (JSON and form).
const DefaultMaxBodyBytes = 100 * 1024

// parseBody decodes JSON and urlencoded request bodies (other content types
// are left unparsed) and stores the result on the request context.
func (s *Server) parseBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
			next.ServeHTTP(w, r)
			return
		}
		mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		isJSON := mt == "application/json" || strings.HasSuffix(mt, "+json")
		isForm := mt == "application/x-www-form-urlencoded"
		if !isJSON && !isForm {
			next.ServeHTTP(w, r)
			return
		}

		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.opts.MaxBodyBytes))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				s.renderError(w, http.StatusRequestEntityTooLarge, "request entity too large")
			} else {
				s.renderError(w, http.StatusBadRequest, "request aborted")
			}
			return
		}

		var body any
		if isJSON {
			body, err = decodeJSON(raw)
		} else {
			body, err = decodeForm(raw)
		}
		if err != nil {
			s.renderError(w, http.StatusBadRequest, err.Error())
			return
		}
		next.ServeHTTP(w, withBody(r, body))
	})
}

// decodeJSON accepts only objects and arrays (strict mode); numbers keep
// their original text so they echo back exactly.
func decodeJSON(raw []byte) (any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return nil, fmt.Errorf("Unexpected token '%c', body is not valid JSON object or array", trimmed[0])
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}
	if dec.More() {
		return nil, errors.New("invalid JSON: unexpected data after top-level value")
	}
	return v, nil
}

func decodeForm(raw []byte) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	values, err := url.ParseQuery(string(raw))
	if err != nil && len(values) == 0 {
		return nil, fmt.Errorf("invalid form body: %v", err)
	}
	return valuesToMap(values), nil
}
