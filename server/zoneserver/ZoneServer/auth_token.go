package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const defaultAuthSecret = "a3-dev-secret-change-me"
const tokenIssuer = "projecta3-login"
const tokenVersion = 1

type tokenClaims struct {
	Username string `json:"username"`
	Iss      string `json:"iss"`
	Ver      int    `json:"ver"`
	Iat      int64  `json:"iat"`
	Exp      int64  `json:"exp"`
}

func authSecret() string {
	secret := strings.TrimSpace(os.Getenv("A3_AUTH_SECRET"))
	if secret == "" {
		return defaultAuthSecret
	}
	return secret
}

func validateAuthConfig() error {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("A3_ENV")))
	secret := strings.TrimSpace(os.Getenv("A3_AUTH_SECRET"))
	if env == "prod" || env == "production" {
		if secret == "" || secret == defaultAuthSecret {
			return errors.New("A3_AUTH_SECRET must be set to a non-default value in production")
		}
	}
	return nil
}

func validateAuthToken(token string) (tokenClaims, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 2 {
		return tokenClaims{}, errors.New("invalid token format")
	}
	payloadEnc := parts[0]
	sig := parts[1]

	expected := signTokenPayload(payloadEnc, authSecret())
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return tokenClaims{}, errors.New("invalid token signature")
	}

	payload, err := base64.RawURLEncoding.DecodeString(payloadEnc)
	if err != nil {
		return tokenClaims{}, fmt.Errorf("decode payload: %w", err)
	}
	var claims tokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return tokenClaims{}, fmt.Errorf("invalid payload: %w", err)
	}
	if strings.TrimSpace(claims.Username) == "" {
		return tokenClaims{}, errors.New("missing username claim")
	}
	if claims.Iss != tokenIssuer {
		return tokenClaims{}, errors.New("invalid token issuer")
	}
	if claims.Ver != tokenVersion {
		return tokenClaims{}, errors.New("unsupported token version")
	}
	now := time.Now().UTC().Unix()
	if claims.Iat > now+60 {
		return tokenClaims{}, errors.New("invalid token issue time")
	}
	if now > claims.Exp {
		return tokenClaims{}, errors.New("token expired")
	}
	return claims, nil
}

func signTokenPayload(payloadEnc, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payloadEnc))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
