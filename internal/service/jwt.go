package service

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type CustomClaims struct {
	Plan     string `json:"plan"`
	Autobots struct {
		SubscriptionStartDate string `json:"subscription_start_date"`
	} `json:"autobots"`
}

type JWTPayload struct {
	Subject      string       `json:"sub"`
	ClientID     string       `json:"client_id"`
	Email        string       `json:"email"`
	CustomClaims CustomClaims `json:"customClaims"`
	IssuedAt     int64        `json:"iat"`
	Expiration   int64        `json:"exp"`
}

func ParseJWT(tokenString string) (*JWTPayload, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token format")
	}

	payloadPart := parts[1]
	
	// Add padding if missing
	if l := len(payloadPart) % 4; l > 0 {
		payloadPart += strings.Repeat("=", 4-l)
	}

	decoded, err := base64.URLEncoding.DecodeString(payloadPart)
	if err != nil {
		// Try standard encoding if URL encoding fails
		decoded, err = base64.StdEncoding.DecodeString(payloadPart)
		if err != nil {
			return nil, err
		}
	}

	var payload JWTPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil, err
	}

	return &payload, nil
}

func GetSubscriptionDate(payload *JWTPayload) time.Time {
	// Try to parse SubscriptionStartDate from CustomClaims
	if v := payload.CustomClaims.Autobots.SubscriptionStartDate; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02", v); err == nil {
			return t
		}
	}
	
	// Fallback to IssuedAt
	if payload.IssuedAt > 0 {
		return time.Unix(payload.IssuedAt, 0)
	}

	return time.Now()
}
