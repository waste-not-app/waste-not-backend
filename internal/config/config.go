package config

import (
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Release                                                              string
	Address, DatabaseURL, Issuer, Audience                               string
	Secret                                                               []byte
	GoogleWebClientID                                                    string
	AIEnabled, RecommendationEnabled, AllowPrototype                     bool
	SwaggerEnabled                                                       bool
	DetectionURL, RecommendationURL, DetectionToken, RecommendationToken string
	PhotoStorageDir                                                      string
	SMTPHost, SMTPUsername, SMTPPassword, SMTPFrom                       string
	SMTPPort                                                             int
}

func Load() (Config, error) {
	c := Config{Release: os.Getenv("WASTENOT_RELEASE"), Address: env("HTTP_ADDR", "127.0.0.1:8080"), DatabaseURL: os.Getenv("DATABASE_URL"), Issuer: "wastenot-api", Audience: "wastenot-android",
		GoogleWebClientID: strings.TrimSpace(os.Getenv("WASTENOT_GOOGLE_WEB_CLIENT_ID")),
		SwaggerEnabled:    os.Getenv("SWAGGER_ENABLED") == "true",
		AIEnabled:         os.Getenv("AI_ENABLED") == "true", RecommendationEnabled: env("AI_RECOMMENDATION_ENABLED", os.Getenv("AI_ENABLED")) == "true", AllowPrototype: os.Getenv("AI_ALLOW_PROTOTYPE") == "true",
		DetectionURL: os.Getenv("AI_DETECTION_URL"), RecommendationURL: os.Getenv("AI_RECOMMENDATION_URL"),
		DetectionToken: os.Getenv("AI_SERVICE_TOKEN"), RecommendationToken: os.Getenv("AI_RECOMMENDATION_TOKEN"),
		PhotoStorageDir: strings.TrimSpace(os.Getenv("PHOTO_STORAGE_DIR"))}
	var err error
	c.SMTPHost = strings.TrimSpace(os.Getenv("SMTP_HOST"))
	c.SMTPUsername = os.Getenv("SMTP_USERNAME")
	c.SMTPPassword = os.Getenv("SMTP_PASSWORD")
	c.SMTPFrom = strings.TrimSpace(os.Getenv("SMTP_FROM"))
	c.SMTPPort, err = strconv.Atoi(env("SMTP_PORT", "587"))
	if err != nil || c.SMTPPort < 1 || c.SMTPPort > 65535 {
		return c, errors.New("SMTP_PORT must be between 1 and 65535")
	}
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
