package main

import (
	"strings"
	"testing"
)

func TestCommandFromArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    string
		wantErr bool
	}{
		{
			name: "default is up",
			args: nil,
			want: "up",
		},
		{
			name: "up",
			args: []string{"up"},
			want: "up",
		},
		{
			name: "verify",
			args: []string{"verify"},
			want: "verify",
		},
		{
			name:    "unknown",
			args:    []string{"drop"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := commandFromArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestDBConfigFromEnv(t *testing.T) {
	t.Setenv("DB_HOST", "writer.example.local")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "app_user")
	t.Setenv("DB_PASSWORD", "app_password")
	t.Setenv("DB_NAME", "app_db")

	cfg, err := dbConfigFromEnv()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.Host != "writer.example.local" {
		t.Fatalf("expected host writer.example.local, got %q", cfg.Host)
	}
}

func TestDBConfigFromEnvMissingValue(t *testing.T) {
	t.Setenv("DB_HOST", "")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "app_user")
	t.Setenv("DB_PASSWORD", "app_password")
	t.Setenv("DB_NAME", "app_db")

	if _, err := dbConfigFromEnv(); err == nil {
		t.Fatal("expected error")
	}
}

func TestPostgresDSN(t *testing.T) {
	cfg := dbConfig{
		Host:     "writer.example.local",
		Port:     "5432",
		User:     "app_user",
		Password: "app_password",
		Name:     "app_db",
	}

	dsn := postgresDSN(cfg)

	if !strings.HasPrefix(dsn, "postgres://app_user:app_password@writer.example.local:5432/app_db?") {
		t.Fatalf("unexpected DSN prefix: %q", dsn)
	}

	if !strings.Contains(dsn, "sslmode=require") {
		t.Fatalf("expected sslmode=require in DSN: %q", dsn)
	}
}

func TestCreateUsersTableSQL(t *testing.T) {
	required := []string{
		"CREATE TABLE IF NOT EXISTS users",
		"id BIGSERIAL PRIMARY KEY",
		"email TEXT NOT NULL UNIQUE",
	}

	for _, fragment := range required {
		if !strings.Contains(createUsersTableSQL, fragment) {
			t.Fatalf("expected migration SQL to contain %q", fragment)
		}
	}
}

func TestVerifyUsersTableSQL(t *testing.T) {
	required := []string{
		"information_schema.tables",
		"table_schema = 'public'",
		"table_name = 'users'",
	}

	for _, fragment := range required {
		if !strings.Contains(verifyUsersTableSQL, fragment) {
			t.Fatalf("expected verify SQL to contain %q", fragment)
		}
	}
}
