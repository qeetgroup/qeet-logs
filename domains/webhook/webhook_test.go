package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSign(t *testing.T) {
	body := []byte(`{"event":"incident.opened"}`)
	got := Sign("s3cr3t", body)

	mac := hmac.New(sha256.New, []byte("s3cr3t"))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if got != want {
		t.Errorf("Sign = %q, want %q", got, want)
	}
	if Sign("", body) != "" {
		t.Error("empty secret must yield empty signature")
	}
}

func TestDeliverSignsAndSetsHeaders(t *testing.T) {
	var gotSig, gotEvent, gotID, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Qeet-Signature")
		gotEvent = r.Header.Get("X-Qeet-Event")
		gotID = r.Header.Get("X-Qeet-Webhook-Id")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	body := []byte(`{"incident_id":"abc"}`)
	ep := Endpoint{ID: "wh_1", URL: srv.URL, Secret: "topsecret"}
	if err := deliver(context.Background(), ep, "incident.opened", body); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if gotEvent != "incident.opened" || gotID != "wh_1" {
		t.Errorf("headers: event=%q id=%q", gotEvent, gotID)
	}
	if gotSig != "sha256="+Sign("topsecret", body) {
		t.Errorf("signature = %q", gotSig)
	}
	if gotBody != string(body) {
		t.Errorf("body = %q", gotBody)
	}
}

func TestDeliverNoRetryOn4xx(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	err := deliver(context.Background(), Endpoint{ID: "x", URL: srv.URL}, "e", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error on 4xx")
	}
	if attempts != 1 {
		t.Errorf("4xx must not retry: got %d attempts", attempts)
	}
}
