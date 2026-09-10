package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type responseLogWriter struct {
	http.ResponseWriter
	statusCode int
	bytes      int
}

func (writer *responseLogWriter) WriteHeader(statusCode int) {
	if writer.statusCode == 0 {
		writer.statusCode = statusCode
	}
	writer.ResponseWriter.WriteHeader(statusCode)
}

func (writer *responseLogWriter) Write(data []byte) (int, error) {
	if writer.statusCode == 0 {
		writer.statusCode = http.StatusOK
	}
	written, err := writer.ResponseWriter.Write(data)
	writer.bytes += written
	return written, err
}

func logHTTPRequest(logger *slog.Logger, route string, handler http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		correlationID := resolveCorrelationID(request.Header.Get(correlationHeader))
		request.Header.Set(correlationHeader, correlationID)
		writer.Header().Set(correlationHeader, correlationID)
		logWriter := &responseLogWriter{ResponseWriter: writer}
		handler(logWriter, request)
		statusCode := logWriter.statusCode
		if statusCode == 0 {
			statusCode = http.StatusOK
		}
		logger.InfoContext(request.Context(), "HTTP request completed", "event", "http.request.completed",
			"http_method", request.Method, "route", route, "status_code", statusCode,
			"duration_ms", durationMilliseconds(time.Since(started)), "response_bytes", logWriter.bytes, "correlation_id", correlationID)
	}
}

func resolveCorrelationID(value string) string {
	if isUUIDv7(value) {
		return strings.ToLower(value)
	}
	return newUUIDv7()
}

func isUUIDv7(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	encoded := value[:8] + value[9:13] + value[14:18] + value[19:23] + value[24:]
	decoded, err := hex.DecodeString(encoded)
	return err == nil && decoded[6]>>4 == 7 && decoded[8]&0xc0 == 0x80
}

func newUUIDv7() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(fmt.Errorf("generate correlation ID: %w", err))
	}
	milliseconds := uint64(time.Now().UnixMilli())
	id[0], id[1], id[2] = byte(milliseconds>>40), byte(milliseconds>>32), byte(milliseconds>>24)
	id[3], id[4], id[5] = byte(milliseconds>>16), byte(milliseconds>>8), byte(milliseconds)
	id[6] = id[6]&0x0f | 0x70
	id[8] = id[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}

func exactGET(path string, handler http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != path {
			http.NotFound(writer, request)
			return
		}
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handler(writer, request)
	}
}

func decodeJSONBody(writer http.ResponseWriter, request *http.Request, limit int64, destination interface{}) error {
	request.Body = http.MaxBytesReader(writer, request.Body, limit)
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(destination); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			http.Error(writer, "request body too large", http.StatusRequestEntityTooLarge)
			return err
		}
		http.Error(writer, "invalid JSON request", http.StatusBadRequest)
		return err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			http.Error(writer, "request body must contain one JSON value", http.StatusBadRequest)
			return errors.New("request body must contain one JSON value")
		}
		http.Error(writer, "invalid JSON request", http.StatusBadRequest)
		return err
	}
	return nil
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload interface{}) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	if err := json.NewEncoder(writer).Encode(payload); err != nil {
		slog.Error("encode response failed", "event", "response.encode_failed", "error", err)
	}
}
