package config

import "testing"

func TestLoadRequiresDatabaseAndTokens(t *testing.T) {
	t.Setenv("ANALYTICS_DATABASE_URL", "")
	t.Setenv("ANALYTICS_INGEST_TOKEN", "")
	t.Setenv("ANALYTICS_QUERY_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected missing configuration error")
	}
}

func TestLoadRejectsIdenticalProductionTokens(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("ANALYTICS_DATABASE_URL", "postgres://localhost/analytics")
	t.Setenv("ANALYTICS_INGEST_TOKEN", "same-token")
	t.Setenv("ANALYTICS_QUERY_TOKEN", "same-token")
	if _, err := Load(); err == nil {
		t.Fatal("expected identical production tokens to be rejected")
	}
}

func TestLoadRejectsInvalidDatabaseURLAndLimits(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	t.Setenv("ANALYTICS_DATABASE_URL", "http://localhost/analytics")
	t.Setenv("ANALYTICS_INGEST_TOKEN", "ingest")
	t.Setenv("ANALYTICS_QUERY_TOKEN", "query")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid database URL to be rejected")
	}

	t.Setenv("ANALYTICS_DATABASE_URL", "postgres://localhost/analytics")
	t.Setenv("ANALYTICS_MAXIMUM_BATCH_SIZE", "0")
	if _, err := Load(); err == nil {
		t.Fatal("expected non-positive batch size to be rejected")
	}
}
