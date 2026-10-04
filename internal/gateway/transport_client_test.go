package gateway

import "testing"

func TestCommandClientHasNoHiddenTotalTimeout(t *testing.T) {
	client := newClient(stateCache{})
	if client.httpClient.Timeout != 0 {
		t.Fatalf("command client timeout = %s, want caller-controlled deadline", client.httpClient.Timeout)
	}
}
