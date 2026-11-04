package config

import (
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	Address, DatabaseURL, Issuer, Audience                               string
	Secret                                                               []byte
	AIEnabled, AllowPrototype                                            bool
	DetectionURL, RecommendationURL, DetectionToken, RecommendationToken string
}

func Load() (Config, error) {
	c := Config{Address: env("HTTP_ADDR", "127.0.0.1:8080"), DatabaseURL: os.Getenv("DATABASE_URL"), Issuer: "wastenot-api", Audience: "wastenot-android",
		AIEnabled: os.Getenv("AI_ENABLED") == "true", AllowPrototype: os.Getenv("AI_ALLOW_PROTOTYPE") == "true",
		DetectionURL: os.Getenv("AI_DETECTION_URL"), RecommendationURL: os.Getenv("AI_RECOMMENDATION_URL"),
		DetectionToken: os.Getenv("AI_SERVICE_TOKEN"), RecommendationToken: os.Getenv("AI_RECOMMENDATION_TOKEN")}
	var err error
	c.Secret, err = base64.StdEncoding.DecodeString(os.Getenv("JWT_SECRET_BASE64"))
	if err != nil || len(c.Secret) < 32 {
		return c, errors.New("JWT_SECRET_BASE64 requires at least 32 random bytes encoded as base64")
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	if c.AIEnabled {
		for _, s := range []string{c.DetectionURL, c.RecommendationURL} {
			u, e := url.Parse(s)
			if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || !strings.Contains(" http https ", " "+u.Scheme+" ") {
				return c, errors.New("AI URLs must be http(s) service origins")
			}
		}
		if len(c.DetectionToken) < 24 || len(c.RecommendationToken) < 24 || c.DetectionToken == c.RecommendationToken {
			return c, errors.New("AI service tokens must be distinct and at least 24 characters")
		}
	}
	return c, nil
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
