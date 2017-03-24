package goworker

import (
	"testing"
)

func TestParseSettingsURI(t *testing.T) {
	redisSettings := RedisSettings{
		URI: "redis://user:pass@host:port/db",
	}
	err := parseSettingsURI(&redisSettings)
	if err != nil {
		t.Errorf("error %s", err)
	}
	if redisSettings.Scheme != "redis" {
		t.Errorf("Expected %s, actual %s", "redis", redisSettings.Scheme)
	}
	if redisSettings.Host != "host:port" {
		t.Errorf("Expected %s, actual %s", "host:port", redisSettings.Host)
	}
	if redisSettings.DB != "db" {
		t.Errorf("Expected %s, actual %s", "db", redisSettings.DB)
	}
	if redisSettings.Password != "pass" {
		t.Errorf("Expected %s, actual %s", "pass", redisSettings.Password)
	}
}
