package mesh_test

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// freeAddr is a loopback address nothing listens on just now.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return "127.0.0.1:" + strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

// eventually retries f until it holds or 15s pass.
func eventually(t *testing.T, what string, f func() error) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		err := f()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %v", what, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// A hub and a leaf as bmesh hub starts them: JetStream on the hub is read
// and written through the leaf, the token is required, and the leaf link
// comes back after the leaf restarts and after the hub restarts on its store.
func TestHubAndLeaf(t *testing.T) {
	const tok = "s3cret-token"
	store := t.TempDir()
	hubAddr, leafPort, leafAddr := freeAddr(t), freeAddr(t), freeAddr(t)
	hubOpts := mesh.HubOptions{Name: "hubbox", Token: tok, Listen: hubAddr, Domain: "hub", Store: store,
		MaxStore: 1 << 30, MaxMemory: 64 << 20, LeafListen: leafPort}
	leafOpts := mesh.HubOptions{Name: "leafbox", Token: tok, Listen: leafAddr, Remote: "nats://" + leafPort}
	hub, err := mesh.StartHub(hubOpts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { hub.Shutdown() }()
	leaf, err := mesh.StartHub(leafOpts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { leaf.Shutdown() }()
	linked := func() error {
		if n := hub.Leaves(); n != 1 {
			return fmt.Errorf("hub has %d leaves", n)
		}
		return nil
	}
	eventually(t, "leaf link", linked)

	ctx := context.Background()
	if _, err := mesh.Connect(ctx, leaf.ClientURL(), "wrong", "hub", "t", false); err == nil {
		t.Fatal("a wrong token connected to the leaf")
	}
	if _, err := mesh.Connect(ctx, hub.ClientURL(), "", "hub", "t", false); err == nil {
		t.Fatal("no token connected to the hub")
	}
	c, err := mesh.Connect(ctx, leaf.ClientURL(), tok, "hub", "t", false)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "stream through the leaf", func() error {
		_, err := c.JS.CreateOrUpdateStream(ctx, jetstream.StreamConfig{Name: "T", Subjects: []string{"t.>"}, Storage: jetstream.FileStorage})
		return err
	})
	if _, err := c.JS.Publish(ctx, "t.one", []byte("first")); err != nil {
		t.Fatal(err)
	}
	c.Close()

	// readBack reads the message through the leaf with a fresh client.
	readBack := func() error {
		c, err := mesh.Connect(ctx, leaf.ClientURL(), tok, "hub", "t", false)
		if err != nil {
			return err
		}
		defer c.Close()
		s, err := c.JS.Stream(ctx, "T")
		if err != nil {
			return err
		}
		m, err := s.GetLastMsgForSubject(ctx, "t.one")
		if err != nil {
			return err
		}
		if string(m.Data) != "first" {
			return fmt.Errorf("read %q", m.Data)
		}
		return nil
	}
	eventually(t, "read through the leaf", readBack)

	// The leaf stops and starts again on its port: the link comes back.
	leaf.Shutdown()
	eventually(t, "link down", func() error {
		if n := hub.Leaves(); n != 0 {
			return fmt.Errorf("hub still has %d leaves", n)
		}
		return nil
	})
	if leaf, err = mesh.StartHub(leafOpts); err != nil {
		t.Fatal(err)
	}
	eventually(t, "leaf relinks", linked)
	eventually(t, "read after the leaf restarts", readBack)

	// The hub stops and starts again on its store: the leaf redials it.
	hub.Shutdown()
	time.Sleep(1500 * time.Millisecond) // past a leaf reconnect attempt or two
	if hub, err = mesh.StartHub(hubOpts); err != nil {
		t.Fatal(err)
	}
	eventually(t, "leaf redials the hub", linked)
	eventually(t, "read after the hub restarts", readBack)
}
