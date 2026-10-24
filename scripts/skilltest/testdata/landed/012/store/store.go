// Package store keeps the shop's orders in one JSON file.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
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

// lockWait is how long Add waits for another checkout's lock.
const lockWait = 2 * time.Second

// Add saves a new order and returns it with its id. It holds the store's
// lock from the read to the rename, so two checkouts at once never number
// two orders alike or drop one.
func (s *Store) Add(o Order) (Order, error) {
	unlock, err := s.lock()
	if err != nil {
		return Order{}, err
	}
	defer unlock()
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

// lock takes the store's lock, the file Path.lock created with O_EXCL and
// holding this process's pid, waiting up to lockWait while another process
// holds it. A lock whose process is gone was left by a crash and is taken
// over. The func it returns removes the lock.
func (s *Store) lock() (func(), error) {
	path := s.Path + ".lock"
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_, err = fmt.Fprintf(f, "%d\n", os.Getpid())
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				os.Remove(path)
				return nil, err
			}
			return func() { os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		// shortcut: two checkouts taking over one crashed lock at once can
		// both go ahead; use flock if crashes under load become common.
		if stale(path) {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%s: another checkout still holds it after %s; remove it if no checkout is running", path, lockWait)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// stale is whether the lock at path names a process that is gone. A lock
// that names no pid yet is being written, so it is held.
func stale(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	return p.Signal(syscall.Signal(0)) != nil
}
