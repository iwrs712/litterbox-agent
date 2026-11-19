package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// decodeJSONBody decodes one bounded JSON object for the private envd APIs.
// Keeping this helper independent from any individual capability prevents
// removing one API from accidentally removing shared request validation.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any, limit int64) error {
	if limit <= 0 {
		return fmt.Errorf("invalid request size limit")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain one JSON value")
		}
		return err
	}
	return nil
}
