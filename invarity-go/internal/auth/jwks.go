package auth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// JWK represents a JSON Web Key.
type JWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
	Alg string `json:"alg"`
}

// JWKS represents a JSON Web Key Set.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWKSCache caches JWKS keys with TTL-based refresh.
type JWKSCache struct {
	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	jwksURL   string
	expiresAt time.Time
	ttl       time.Duration
	client    *http.Client
}

// NewJWKSCache creates a new JWKS cache.
func NewJWKSCache(jwksURL string, ttl time.Duration) *JWKSCache {
	return &JWKSCache{
		keys:    make(map[string]*rsa.PublicKey),
		jwksURL: jwksURL,
		ttl:     ttl,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetKey retrieves a public key by key ID, refreshing the cache if needed.
func (c *JWKSCache) GetKey(kid string) (*rsa.PublicKey, error) {
	c.mu.RLock()
	if time.Now().Before(c.expiresAt) {
		if key, ok := c.keys[kid]; ok {
			c.mu.RUnlock()
			return key, nil
		}
	}
	c.mu.RUnlock()

	// Need to refresh cache
	if err := c.refresh(); err != nil {
		return nil, fmt.Errorf("failed to refresh JWKS: %w", err)
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	key, ok := c.keys[kid]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", kid)
	}
	return key, nil
}

func (c *JWKSCache) refresh() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if time.Now().Before(c.expiresAt) {
		return nil
	}

	resp, err := c.client.Get(c.jwksURL)
	if err != nil {
		return fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS fetch returned status %d", resp.StatusCode)
	}

	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("failed to decode JWKS: %w", err)
	}

	newKeys := make(map[string]*rsa.PublicKey)
	for _, jwk := range jwks.Keys {
		if jwk.Kty != "RSA" || jwk.Use != "sig" {
			continue
		}

		key, err := jwkToRSAPublicKey(jwk)
		if err != nil {
			continue // Skip invalid keys
		}
		newKeys[jwk.Kid] = key
	}

	c.keys = newKeys
	c.expiresAt = time.Now().Add(c.ttl)
	return nil
}

func jwkToRSAPublicKey(jwk JWK) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode N: %w", err)
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode E: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := int(new(big.Int).SetBytes(eBytes).Int64())

	return &rsa.PublicKey{
		N: n,
		E: e,
	}, nil
}
