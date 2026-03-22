package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrTokenExpired = errors.New("token expired")
	ErrTokenInvalid = errors.New("invalid token")
)

// Claims holds JWT payload.
type Claims struct {
	IssuedAt  int64 `json:"iat"`
	ExpiresAt int64 `json:"exp"`
}

const defaultExpiry = 24 * time.Hour

// SignJWT creates a JWT token with HMAC-SHA256 signature.
func SignJWT(secret []byte, expiry time.Duration) (string, error) {
	if expiry == 0 {
		expiry = defaultExpiry
	}
	now := time.Now().Unix()
	claims := Claims{
		IssuedAt:  now,
		ExpiresAt: now + int64(expiry.Seconds()),
	}

	header := base64Encode([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	payloadB64 := base64Encode(payload)

	sigInput := header + "." + payloadB64
	sig := hmacSign([]byte(sigInput), secret)

	return sigInput + "." + base64Encode(sig), nil
}

// VerifyJWT validates a JWT token and returns claims.
func VerifyJWT(token string, secret []byte) (*Claims, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return nil, ErrTokenInvalid
	}

	sigInput := parts[0] + "." + parts[1]
	expectedSig := hmacSign([]byte(sigInput), secret)
	actualSig, err := base64Decode(parts[2])
	if err != nil {
		return nil, ErrTokenInvalid
	}

	if !hmac.Equal(expectedSig, actualSig) {
		return nil, ErrTokenInvalid
	}

	payloadBytes, err := base64Decode(parts[1])
	if err != nil {
		return nil, ErrTokenInvalid
	}

	var claims Claims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, ErrTokenInvalid
	}

	if time.Now().Unix() > claims.ExpiresAt {
		return nil, ErrTokenExpired
	}

	return &claims, nil
}

func hmacSign(data, secret []byte) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write(data)
	return h.Sum(nil)
}

func base64Encode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func base64Decode(s string) ([]byte, error) {
	// Handle both padded and unpadded.
	if len(s)%4 != 0 {
		s += strings.Repeat("=", 4-len(s)%4)
	}
	return base64.URLEncoding.DecodeString(s)
}

// MaskKey returns a masked version of an API key for display.
func MaskKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "***" + key[len(key)-4:]
}

