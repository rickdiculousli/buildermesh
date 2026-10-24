// Package store keeps the shop's orders in one JSON file.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"example.com/shop/cart"
)

// Order is one checkout as saved.
type Order struct {
	ID    int         `json:"id"`
	At    time.Time   `json:"at"`
	Items []cart.Item `json:"items"`
	Code  string      `json:"code,omitempty"`
	Total int64       `json:"total"`
}

// Store is the orders file.
type Store struct {
	Path string
}

// Open returns the store at path; nothing is read until Orders or Add.
func Open(path string) *Store { return &Store{Path: path} }

// Orders reads every order, oldest first; a missing file is no orders.
func (s *Store) Orders() ([]Order, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var orders []Order
	if err := json.Unmarshal(b, &orders); err != nil {
		return nil, fmt.Errorf("%s: %w", s.Path, err)
	}
	return orders, nil
}

// Add saves a new order and returns it with its id.
func (s *Store) Add(o Order) (Order, error) {
	orders, err := s.Orders()
	if err != nil {
		return Order{}, err
	}
	o.ID = len(orders) + 1
	orders = append(orders, o)
	b, err := json.MarshalIndent(orders, "", "  ")
	if err != nil {
		return Order{}, err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return Order{}, err
	}
	if err := os.Rename(tmp, s.Path); err != nil {
		return Order{}, err
	}
	return o, nil
}
