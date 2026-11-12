package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
	"github.com/milinkovicluka/gator/internal/database"
)

// handlerAPIKey creates a new API key for the current user.
// Only a hash is stored, so the key is shown once and can't be shown again;
// running the command again replaces the old key.
func handlerAPIKey(s *state, cmd command, user database.User) error {
	if len(cmd.args) > 0 {
		return fmt.Errorf("usage: %s", cmd.name)
	}

	key, err := newAPIKey(context.Background(), s.db, user.ID)
	if err != nil {
		return err
	}

	if user.ApiKeyHash.Valid {
		fmt.Println("Your old API key no longer works.")
	}
	fmt.Printf("New API key for %s:\n\n  %s\n\n", user.Name, key)
	fmt.Println("Save it somewhere safe: it won't be shown again.")
	fmt.Println("Use it like this:")
	fmt.Printf("  curl -H \"Authorization: Bearer %s\" http://localhost:8080/v1/users/me\n", key)
	return nil
}

// newAPIKey generates a random key, stores its hash and returns the key.
func newAPIKey(ctx context.Context, db *database.Queries, userID uuid.UUID) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("couldn't generate API key: %w", err)
	}
	key := "gtr_" + hex.EncodeToString(b)

	err := db.SetUserAPIKeyHash(ctx, database.SetUserAPIKeyHashParams{
		ID:         userID,
		ApiKeyHash: sql.NullString{String: hashAPIKey(key), Valid: true},
	})
	if err != nil {
		return "", fmt.Errorf("couldn't save API key: %w", err)
	}
	return key, nil
}

func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
