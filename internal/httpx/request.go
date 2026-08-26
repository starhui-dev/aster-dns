package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

const maximumJSONBodyBytes = 1 << 20

func DecodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	contentType := strings.TrimSpace(r.Header.Get("Content-Type"))
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return errors.New("request content type must be application/json")
	}
	if r.ContentLength > maximumJSONBodyBytes {
		return errors.New("request body is too large")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maximumJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("invalid JSON request")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON value")
	}
	return nil
}
