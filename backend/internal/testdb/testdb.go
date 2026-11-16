// Package testdb prepares an isolated PostgreSQL database for integration
// tests. Tests are skipped unless TEST_DATABASE_URL is set, so the default
// `go test ./...` run never touches a developer database.
package testdb

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/store"
)

const (
	envURL    = "TEST_DATABASE_URL"
	openLimit = 10 * time.Second
)

// Spawn creates a throwaway database, applies backend/migrations and returns a
// connected store scoped to it. The database is dropped when the test ends.
func Spawn(t *testing.T) *store.Store {
	t.Helper()
	adminDSN := strings.TrimSpace(os.Getenv(envURL))
	if adminDSN == "" {
		t.Skipf("%s is not set: skipping PostgreSQL integration test", envURL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), openLimit)
	defer cancel()

	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatalf("connect admin database: %v", err)
	}
	name := fmt.Sprintf("avito_test_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		admin.Close(context.Background())
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), openLimit)
		defer cancel()
		defer admin.Close(cleanupCtx)
		if _, err := admin.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)"); err != nil {
			t.Logf("drop test database %s: %v", name, err)
		}
	})

	dsn, err := withDatabase(adminDSN, name)
	if err != nil {
		t.Fatalf("build test dsn: %v", err)
	}
	applyMigrations(t, ctx, dsn)
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(st.Close)
	return st
}

func withDatabase(dsn, name string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	parsed.Path = "/" + name
	return parsed.String(), nil
}

func applyMigrations(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate testdb source")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "migrations")
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no migrations found in %s", dir)
	}
	sort.Strings(files)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	defer conn.Close(ctx)
	for _, file := range files {
		sql, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", filepath.Base(file), err)
		}
		// No parameters: pgx uses the simple protocol and accepts the whole
		// multi-statement migration, including dollar-quoted function bodies.
		if _, err := conn.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply migration %s: %v", filepath.Base(file), err)
		}
	}
}
