package llmgateway

import (
	"database/sql"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/picoaide/picoaide/internal/serverstore"
)

type UpstreamKey struct {
	ID  int64
	Key string
}

type keyLease struct {
	pool       *providerKeyPool
	providerID int64
	keyID      int64
	apiKey     string
	once       sync.Once
}

func (l *keyLease) APIKey() string { return l.apiKey }
func (l *keyLease) Success()       { l.finish(0, 0, false) }
func (l *keyLease) Failure(status int, retryAfter string) {
	l.finish(status, parseRetryAfter(retryAfter), true)
}
func (l *keyLease) NetworkFailure() { l.finish(0, 5*time.Second, true) }
func (l *keyLease) finish(status int, retryAfter time.Duration, failed bool) {
	if l == nil {
		return
	}
	l.once.Do(func() { l.pool.finish(l.providerID, l.keyID, status, retryAfter, failed) })
}

type providerKeyPool struct {
	db   *sql.DB
	mu   sync.Mutex
	next map[int64]int
}

func (p *providerKeyPool) acquireLease(providerID int64, fallback string) (*keyLease, error) {
	return p.acquire(providerID, fallback)
}

func newProviderKeyPool(db *sql.DB) *providerKeyPool {
	return &providerKeyPool{db: db, next: make(map[int64]int)}
}

func (p *providerKeyPool) acquire(providerID int64, fallback string) (*keyLease, error) {
	keys, err := serverstore.ListGatewayProviderAPIKeys(p.db, providerID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	available := make([]serverstore.GatewayProviderAPIKey, 0, len(keys))
	for _, key := range keys {
		if key.Enabled && (key.CooldownUntil == nil || !key.CooldownUntil.After(now)) {
			available = append(available, key)
		}
	}
	if len(available) == 0 {
		if len(keys) > 0 {
			return nil, errors.New("all upstream API keys are cooling down or disabled")
		}
		if fallback == "" {
			return nil, errors.New("no upstream API key available")
		}
		return &keyLease{pool: p, providerID: providerID, apiKey: fallback}, nil
	}
	p.mu.Lock()
	index := p.next[providerID] % len(available)
	p.next[providerID] = (index + 1) % len(available)
	p.mu.Unlock()
	key := available[index]
	plain, err := DecryptSecret(key.APIKeyEnc)
	if err != nil {
		return nil, err
	}
	return &keyLease{pool: p, providerID: providerID, keyID: key.ID, apiKey: plain}, nil
}

func (p *providerKeyPool) finish(providerID, keyID int64, status int, retryAfter time.Duration, failed bool) {
	if keyID == 0 {
		return
	}
	if !failed {
		_, _ = p.db.Exec(`UPDATE gateway_provider_api_keys SET failure_count = 0, cooldown_until = NULL, last_used_at = now(), updated_at = now() WHERE provider_id = ? AND id = ?`, providerID, keyID)
		return
	}
	var count int
	_ = p.db.QueryRow(`SELECT failure_count FROM gateway_provider_api_keys WHERE provider_id = ? AND id = ?`, providerID, keyID).Scan(&count)
	count++
	cooldown := retryAfter
	if cooldown <= 0 {
		switch status {
		case http.StatusUnauthorized, http.StatusForbidden:
			cooldown = 10 * time.Minute
		case http.StatusTooManyRequests:
			seconds := math.Pow(2, float64(minInt(count-1, 5))) * 30
			cooldown = time.Duration(seconds) * time.Second
		default:
			cooldown = 5 * time.Second
		}
	}
	if cooldown > 30*time.Minute {
		cooldown = 30 * time.Minute
	}
	_, _ = p.db.Exec(`UPDATE gateway_provider_api_keys SET failure_count = ?, cooldown_until = ?, last_error_at = now(), updated_at = now() WHERE provider_id = ? AND id = ?`, count, time.Now().Add(cooldown), providerID, keyID)
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		return time.Until(at)
	}
	return 0
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (a *API) upstreamWithKey(up Upstream) (Upstream, *keyLease, error) {
	if a == nil || a.keyPool == nil {
		return up, nil, nil
	}
	lease, err := a.keyPool.acquire(up.ID, up.APIKey)
	if err != nil {
		return up, nil, err
	}
	up.APIKey = lease.APIKey()
	return up, lease, nil
}

func recordLeaseResponse(lease *keyLease, resp *http.Response, err error) {
	if lease == nil {
		return
	}
	if err != nil {
		lease.NetworkFailure()
		return
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		lease.Success()
		return
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		lease.Failure(resp.StatusCode, resp.Header.Get("Retry-After"))
		return
	}
	if resp.StatusCode >= 500 {
		lease.NetworkFailure()
		return
	}
	lease.Success()
}
