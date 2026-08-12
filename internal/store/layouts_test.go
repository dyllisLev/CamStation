package store

import (
	"errors"
	"testing"
)

func TestDeleteLayout(t *testing.T) {
	db := openMigratedStore(t)
	created, err := db.CreateLayout(t.Context(), LayoutProfile{
		ID:   "ops",
		Name: "Ops",
		Data: []LayoutItem{{I: "yard", X: 0, Y: 0, W: 24, H: 24}},
	})
	if err != nil {
		t.Fatalf("create layout: %v", err)
	}

	if err := db.DeleteLayout(t.Context(), created.ID); err != nil {
		t.Fatalf("delete layout: %v", err)
	}
	if _, err := db.GetLayout(t.Context(), created.ID); err == nil {
		t.Fatal("get deleted layout returned nil error")
	}
}

func TestDeleteLayoutReturnsNotFound(t *testing.T) {
	db := openMigratedStore(t)
	if err := db.DeleteLayout(t.Context(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteLayout error = %v, want ErrNotFound", err)
	}
}
