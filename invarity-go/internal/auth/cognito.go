package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"crypto"
	"crypto/sha256"
)

// CognitoConfig holds configuration for Cognito JWT verification.
type CognitoConfig struct {
	Issuer   string        // e.g., https://cognito-idp.{region}.amazonaws.com/{userPoolId}
	Audience string        // Client ID (app client)
	Region   string        // AWS region
	JWKSTTL  time.Duration // How long to cache JWKS keys
}

// CognitoVerifier verifies Cognito JWT tokens.
type CognitoVerifier struct {
	config    CognitoConfig
	jwksCache *JWKSCache
}

// NewCognitoVerifier creates a new Cognito JWT verifier.
func NewCognitoVerifier(cfg CognitoConfig) *CognitoVerifier {
	if cfg.JWKSTTL == 0 {
		cfg.JWKSTTL = 1 * time.Hour
	}

	jwksURL := fmt.Sprintf("%s/.well-known/jwks.json", cfg.Issuer)

	return &CognitoVerifier{
		config:    cfg,
		jwksCache: NewJWKSCache(jwksURL, cfg.JWKSTTL),
	}
}

// JWTClaims represents the claims in a Cognito JWT.
type JWTClaims struct {
	Sub       string `json:"sub"`
	Email     string `json:"email"`
	Iss       string `json:"iss"`
	Aud       string `json:"aud"`
	ClientID  string `json:"client_id"` // For access tokens
	TokenUse  string `json:"token_use"` // "id" or "access"
	Exp       int64  `json:"exp"`
	Iat       int64  `json:"iat"`
	AuthTime  int64  `json:"auth_time"`
	Username  string `json:"cognito:username"`
	EventID   string `json:"event_id"`
}

// JWTHeader represents the header of a JWT.
type JWTHeader struct {
	Kid string `json:"kid"`
	Alg string `json:"alg"`
}

// VerifyToken verifies a JWT token and returns the claims.
func (v *CognitoVerifier) VerifyToken(tokenString string) (*JWTClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	// Decode header
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("failed to decode header: %w", err)
	}

	var header JWTHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("failed to parse header: %w", err)
	}

	if header.Alg != "RS256" {
		return nil, fmt.Errorf("unsupported algorithm: %s", header.Alg)
	}

	// Get public key
	pubKey, err := v.jwksCache.GetKey(header.Kid)
	if err != nil {
		return nil, fmt.Errorf("failed to get public key: %w", err)
	}

	// Verify signature
	if err := v.verifySignature(parts[0]+"."+parts[1], parts[2], pubKey); err != nil {
		return nil, fmt.Errorf("signature verification failed: %w", err)
	}

	// Decode claims
	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode claims: %w", err)
	}

	var claims JWTClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse claims: %w", err)
	}

	// Validate claims
	if err := v.validateClaims(&claims); err != nil {
		return nil, err
	}

	return &claims, nil
}

func (v *CognitoVerifier) verifySignature(message, signature string, pubKey *rsa.PublicKey) error {
	sigBytes, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("failed to decode signature: %w", err)
	}

	hash := sha256.Sum256([]byte(message))
	return rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], sigBytes)
}

func (v *CognitoVerifier) validateClaims(claims *JWTClaims) error {
	now := time.Now().Unix()

	// Check expiration
	if claims.Exp < now {
		return fmt.Errorf("token expired")
	}

	// Check issuer
	if claims.Iss != v.config.Issuer {
		return fmt.Errorf("invalid issuer: expected %s, got %s", v.config.Issuer, claims.Iss)
	}

	// Check audience (for ID tokens) or client_id (for access tokens)
	if claims.TokenUse == "id" {
		if claims.Aud != v.config.Audience {
			return fmt.Errorf("invalid audience: expected %s, got %s", v.config.Audience, claims.Aud)
		}
	} else if claims.TokenUse == "access" {
		if claims.ClientID != v.config.Audience {
			return fmt.Errorf("invalid client_id: expected %s, got %s", v.config.Audience, claims.ClientID)
		}
	}

	return nil
}

// Middleware returns an HTTP middleware that verifies Cognito JWT tokens.
func (v *CognitoVerifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, `{"error":"missing authorization header","code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, `{"error":"invalid authorization header format","code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := v.VerifyToken(token)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid token: %s","code":"UNAUTHORIZED"}`, err.Error()), http.StatusUnauthorized)
			return
		}

		// Create auth context
		authCtx := &AuthContext{
			ActorType: ActorTypeUser,
			UserID:    claims.Sub,
			Email:     claims.Email,
		}

		// Add to request context
		ctx := WithAuthContext(r.Context(), authCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuth is a convenience function that returns the middleware.
func (v *CognitoVerifier) RequireAuth() func(http.Handler) http.Handler {
	return v.Middleware
}

// OptionalAuth is a middleware that verifies JWT if present but doesn't require it.
func (v *CognitoVerifier) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			next.ServeHTTP(w, r)
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			next.ServeHTTP(w, r)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := v.VerifyToken(token)
		if err != nil {
			// Invalid token, but optional - continue without auth
			next.ServeHTTP(w, r)
			return
		}

		authCtx := &AuthContext{
			ActorType: ActorTypeUser,
			UserID:    claims.Sub,
			Email:     claims.Email,
		}

		ctx := WithAuthContext(r.Context(), authCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetUserIDFromContext is a helper to get user ID from context.
func GetUserIDFromContext(ctx context.Context) (string, bool) {
	auth := GetAuthContext(ctx)
	if auth == nil || auth.ActorType != ActorTypeUser {
		return "", false
	}
	return auth.UserID, true
}
