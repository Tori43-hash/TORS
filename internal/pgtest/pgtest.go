// Package pgtest gives tests a fresh PostgreSQL database. Set TORS_TEST_DSN
// to a server where the user may create databases; without it tests skip.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// DSN creates a new database for the test and returns its connection string.
// The database is dropped when the test ends.
func DSN(t testing.TB) string {
	t.Helper()
	base := os.Getenv("TORS_TEST_DSN")
	if base == "" {
		t.Skip("TORS_TEST_DSN не задан — тест с PostgreSQL пропущен")
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "tors_test_" + hex.EncodeToString(b)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("PostgreSQL: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), base)
		if err != nil {
			return
		}
		defer c.Close(context.Background())
		_, _ = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}
