package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type closeErrorBody struct{}

func (closeErrorBody) Read([]byte) (int, error) { return 0, io.EOF }
func (closeErrorBody) Close() error             { return errors.New("close failed") }

func TestCheckHealth(t *testing.T) {
	tests := []struct {
		name    string
		client  *http.Client
		wantErr string
	}{
		{
			name:   "healthy response",
			client: http.DefaultClient,
		},
		{
			name: "unhealthy status",
			client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Status: "503 Service Unavailable", Body: io.NopCloser(strings.NewReader("not ready"))}, nil
			})},
			wantErr: "503 Service Unavailable",
		},
		{
			name: "request failure",
			client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("connection refused")
			})},
			wantErr: "connection refused",
		},
		{
			name: "body close failure",
			client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: closeErrorBody{}}, nil
			})},
			wantErr: "close failed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := test.client
			if test.name == "healthy response" {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				defer server.Close()
				if err := checkHealth(client, server.URL); err != nil {
					t.Fatalf("checkHealth returned error: %v", err)
				}
				return
			}
			err := checkHealth(client, "http://cdn.test/health")
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("checkHealth error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestCheckHealthUnhealthyResponseCloseFailure(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Status: "502 Bad Gateway", Body: closeErrorBody{}}, nil
	})}
	err := checkHealth(client, "http://cdn.test/health")
	if err == nil || !strings.Contains(err.Error(), "502 Bad Gateway") || !strings.Contains(err.Error(), "close failed") {
		t.Fatalf("checkHealth error = %v, want status and close error", err)
	}
}

func TestProcessEnvironment(t *testing.T) {
	t.Setenv("ICON_CDN_TEST_VALUE", "present")
	got := processEnvironment()
	if got["ICON_CDN_TEST_VALUE"] != "present" {
		t.Fatalf("environment value = %q", got["ICON_CDN_TEST_VALUE"])
	}
}
