package cluster

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"

	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (s *Service) fleetPeerToken(ctx context.Context, base, node, scope, resource, grant string, revision int64) (string, string, error) {
	return s.exchangeFleetPeerToken(ctx, base, node, scope, resource, grant, revision, true)
}

func (s *Service) exchangeFleetPeerToken(ctx context.Context, base, node, scope, resource, grant string, revision int64, useCache bool) (string, string, error) {
	cacheKey := base + "/" + node + "/" + scope + "/" + resource + "/" + grant + "/" + strconv.FormatInt(revision, 10)
	s.mu.Lock()
	cached := s.fleetTokens[cacheKey]
	s.mu.Unlock()
	if useCache && cached.Token != "" && cached.ExpiresAt > time.Now().Add(15*time.Second).UnixMilli() {
		return cached.Token, cacheKey, nil
	}
	proof := FleetExchangeProof{ConsumerID: grant, NodeID: node, Scope: scope, ResourceID: resource, Timestamp: time.Now().UnixMilli(), Nonce: uuid.NewString()}
	body, _ := json.Marshal(proof)
	s.mu.Lock()
	identity, err := s.fleetIdentity()
	s.mu.Unlock()
	if err != nil {
		return "", cacheKey, err
	}
	key, err := base64.RawURLEncoding.DecodeString(identity.PrivateKey)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return "", cacheKey, ErrUnauthorized
	}
	signed := ed25519.Sign(ed25519.PrivateKey(key), append([]byte("POST /api/node/v1/fleet/execution/tokens/exchange\n"), body...))
	body, _ = json.Marshal(FleetExchangeRequest{Proof: proof, Signature: base64.RawURLEncoding.EncodeToString(signed)})
	raw, err := s.fleetHTTP(ctx, strings.TrimRight(base, "/")+"/api/node/v1/fleet/execution/tokens/exchange", "", body)
	if err != nil {
		return "", cacheKey, err
	}
	var token struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expiresAt"`
	}
	if json.Unmarshal(raw, &token) != nil || token.Token == "" {
		return "", cacheKey, ErrUnauthorized
	}
	s.mu.Lock()
	if s.fleetTokens == nil || len(s.fleetTokens) > 1000 {
		s.fleetTokens = map[string]cachedFleetToken{}
	}
	s.fleetTokens[cacheKey] = cachedFleetToken{Token: token.Token, ExpiresAt: token.ExpiresAt}
	s.mu.Unlock()
	return token.Token, cacheKey, nil
}
func (s *Service) runFleetSharedWorker(work func(context.Context), interval time.Duration) {
	s.runFleetWorker(work, interval, 20*time.Second)
}

func (s *Service) runFleetWorker(work func(context.Context), interval, timeout time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.closed:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			work(ctx)
			cancel()
		}
	}
}
