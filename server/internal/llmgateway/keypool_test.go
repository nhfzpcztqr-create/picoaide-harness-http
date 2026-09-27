package llmgateway

import (
	"testing"
	"time"

	"github.com/picoaide/picoaide/internal/serverstore"
)

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("12"); got != 12*time.Second {
		t.Fatalf("got %v", got)
	}
	if got := parseRetryAfter("bad"); got != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestProviderKeyPoolRotatesAndUsesLegacyFallback(t *testing.T) {
	db, cleanup := serverstore.NewTestDB(t)
	defer cleanup()
	orig := DecryptSecret
	DecryptSecret = func(value string) (string, error) { return value, nil }
	t.Cleanup(func() { DecryptSecret = orig })
	providerID, err := serverstore.AddGatewayProvider(db, &serverstore.GatewayProvider{Name: "pool", BaseURL: "http://up", APIKeyEnc: "legacy", Models: []string{"m"}, Enabled: 1, Protocol: "openai"})
	if err != nil {
		t.Fatal(err)
	}
	first := &serverstore.GatewayProviderAPIKey{ProviderID: providerID, APIKeyEnc: "one", Enabled: true, Priority: 0}
	second := &serverstore.GatewayProviderAPIKey{ProviderID: providerID, APIKeyEnc: "two", Enabled: true, Priority: 0}
	if _, err := serverstore.AddGatewayProviderAPIKey(db, first); err != nil {
		t.Fatal(err)
	}
	if _, err := serverstore.AddGatewayProviderAPIKey(db, second); err != nil {
		t.Fatal(err)
	}
	pool := newProviderKeyPool(db)
	lease1, err := pool.acquire(providerID, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	lease2, err := pool.acquire(providerID, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if lease1.APIKey() == lease2.APIKey() {
		t.Fatalf("keys did not rotate: %q", lease1.APIKey())
	}
	lease1.Success()
	lease2.Failure(429, "1")
	lease3, err := pool.acquire(providerID, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if lease3.APIKey() == lease2.APIKey() {
		t.Fatalf("cooling key was selected")
	}
}
