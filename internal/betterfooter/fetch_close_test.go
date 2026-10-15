package betterfooter

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type quotaCloseFailureBody struct {
	io.Reader
	cause error
}

func (body quotaCloseFailureBody) Close() error { return body.cause }

type quotaCloseFailureTransport struct {
	status int
	cause  error
}

func (transport quotaCloseFailureTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: transport.status,
		Header:     make(http.Header),
		Body:       quotaCloseFailureBody{Reader: strings.NewReader("quota"), cause: transport.cause},
	}, nil
}

func TestHTTPGetCloseFailureIsObservableWithoutExposingCredentials(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized} {
		cause := errors.New("transport close failed with secret-token")
		client := &http.Client{Transport: quotaCloseFailureTransport{status: status, cause: cause}}
		_, err := httpGet(context.Background(), client, "quota", "https://quota.invalid", nil)
		if !errors.Is(err, cause) {
			t.Fatalf("close cause was lost: %v", err)
		}
		if strings.Contains(err.Error(), "secret-token") || !strings.Contains(err.Error(), "response close failed") {
			t.Fatalf("unsafe or missing close diagnostic: %v", err)
		}
		if status == http.StatusUnauthorized && !strings.Contains(err.Error(), "HTTP 401") {
			t.Fatalf("HTTP failure was lost: %v", err)
		}
	}
}
