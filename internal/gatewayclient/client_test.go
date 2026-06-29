package gatewayclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"seamless-cors/internal/gatewaycoord"
	"seamless-cors/internal/gatewayfacade"
)

func TestStartSendsTypedRequestWithOwnerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/start" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get(tokenHeader); got != "owner-token" {
			t.Fatalf("token header = %q", got)
		}
		var request gatewayfacade.StartRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.PACReplacementConsent == nil || !request.PACReplacementConsent.Accepted {
			t.Fatalf("request = %#v", request)
		}
		_ = json.NewEncoder(w).Encode(gatewayfacade.StartResult{Kind: gatewayfacade.StartResultStarted})
	}))
	defer server.Close()

	client := New(gatewaycoord.GatewayStateCache{
		HTTPRouterListen: server.Listener.Addr().String(),
		Token:            "owner-token",
	})
	result, err := client.Start(gatewayfacade.StartRequest{
		PACReplacementConsent: &gatewayfacade.PACReplacementConsentInput{Accepted: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != gatewayfacade.StartResultStarted {
		t.Fatalf("result = %s", result.Kind)
	}
}

func TestStatusReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := New(gatewaycoord.GatewayStateCache{
		HTTPRouterListen: server.Listener.Addr().String(),
		Token:            "bad-token",
	})
	if _, err := client.Status(); err == nil {
		t.Fatal("expected status error")
	}
}
