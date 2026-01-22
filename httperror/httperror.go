// Package httperror simplifies returning an error as JSON from an HTTP handler
package httperror

import (
	"encoding/json"
	"net/http"
)

type jsonError struct {
	Error string `json:"error"`
}

func Send(w http.ResponseWriter, req *http.Request, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	m := jsonError{Error: message}
	json.NewEncoder(w).Encode(m)
}
