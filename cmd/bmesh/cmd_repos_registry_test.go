package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

// registryVersion is the box's registry.json version (0: none).
func registryVersion(b box) int64 {
	l, ok, err := hosts.LoadLocal(filepath.Join(b.state, "bmesh"))
	if err != nil || !ok {
		return 0
	}
	r, _, _ := hosts.LoadRegistry(filepath.Join(b.state, "bmesh"), l.Pinned)
	return r.Version
}

func TestRegistryPublishProposeAndRemoveWithCheckouts(t *testing.T) {
	atScreenForTest(t)
	serveHostsEvery = 300 * time.Millisecond
	t.Cleanup(func() { serveHostsEvery = 0 })
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	url := meshtest.Start(t)
	mac, k16 := newBox(t, "mac", url), newBox(t, "k16", url)
	if out, errs, code := mac.run(t, "y\nn\npw\npw\n", "init", "--hosts", "--admin"); code != 0 {
		t.Fatalf("admin init: %d %s %s", code, out, errs)
	}
	codeStr, wait := invite(t, mac, "pw\n", "k16", "--accept", "ping,nix")
	if out, errs, code := k16.run(t, "\nn\nn\n"+codeStr+"\n", "init", "--hosts"); code != 0 {
		t.Fatalf("join: %d %s %s", code, out, errs)
	}
	if res, c := wait(); c != 0 {
		t.Fatalf("invite: %d %s", c, res)
	}
	if out, errs, code := mac.run(t, "pw\n", "hosts", "set", "mac", "--accept", "ping,nix,propose"); code != 0 {
		t.Fatalf("set: %d %s %s", code, out, errs)
	}

	// The repo, checked out beside each box's config: a relative row path
	// resolves there, so one registry serves both boxes.
	root := t.TempDir()
	origin := filepath.Join(root, "nixrepo.git")
	meshtest.Git(t, root, "init", "-q", "--bare", "-b", "main", origin)
	seed := filepath.Join(root, "seed")
	meshtest.Git(t, root, "clone", "-q", origin, seed)
	meshtest.CommitFiles(t, seed, map[string]string{"a.txt": "one\n"})
	meshtest.Git(t, seed, "push", "-q", "origin", "HEAD:main")
	dir := func(b box) string { return filepath.Join(filepath.Dir(b.cfg), "nixrepo") }
	repos := func(b box) string { return filepath.Join(filepath.Dir(b.cfg), "repos.toml") }
	for _, b := range []box{mac, k16} {
		meshtest.Git(t, root, "clone", "-q", origin, dir(b))
	}
	reg1 := "# the fleet's registry\n\n[repos.nixrepo]\npath = \"nixrepo\"\nremote = \"" + origin + "\"\n"
	if err := os.WriteFile(repos(mac), []byte(reg1), 0o644); err != nil {
		t.Fatal(err)
	}

	// k16 can't publish; the admin can, and k16's daemon takes it.
	if _, errs, code := k16.run(t, "", "repos", "publish"); code != exitRefused || !strings.Contains(errs, "only the admin box") {
		t.Fatalf("publish on k16: %d %s", code, errs)
	}
	out, errs, code := mac.run(t, "pw\n", "repos", "publish")
	if code != 0 || !strings.Contains(out, "registry v0 -> v1:") || !strings.Contains(out, "+ nixrepo: path nixrepo") ||
		!strings.Contains(out, "registry v1 published") {
		t.Fatalf("publish: %d %s %s", code, out, errs)
	}
	if out, _, _ := mac.run(t, "", "repos", "publish"); !strings.Contains(out, "registry v1: no change") {
		t.Fatalf("publish again: %s", out)
	}
	serveBox(t, mac)
	_, stopK16 := serveBox(t, k16)
	waitFor(t, "k16 takes registry v1", func() bool { return readFile(t, repos(k16)) == reg1 })

	// A verify change proposed from k16 is held on the Mac and changes
	// nothing until it is approved.
	reg2 := reg1 + "verify = [\"make check\"]\n"
	if err := os.WriteFile(repos(k16), []byte(reg2), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errs, code = k16.run(t, "", "repos", "propose", "--wait", "20s")
	if code != 0 || !strings.Contains(out, `~ nixrepo: verify: (none) -> ["make check"]`) || !strings.Contains(out, "mac: held for approval as ") {
		t.Fatalf("propose: %d %s %s", code, out, errs)
	}
	out, _, _ = mac.run(t, "", "hosts", "held")
	id := strings.Fields(out)[0]
	if !strings.Contains(out, "registry proposal, 1 row change against v1, from k16") {
		t.Fatalf("held: %s", out)
	}
	if readFile(t, repos(mac)) != reg1 || registryVersion(mac) != 1 {
		t.Fatal("a held proposal changed the registry")
	}
	// Unpublished edits on the admin box would be overwritten: refused.
	if err := os.WriteFile(repos(mac), []byte(reg1+"# wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errs, code := mac.run(t, "pw\n", "hosts", "approve", id); code != exitRefused || !strings.Contains(errs, "which approving would overwrite") {
		t.Fatalf("approve over unpublished edits: %d %s", code, errs)
	}
	if err := os.WriteFile(repos(mac), []byte(reg1), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errs, code = mac.run(t, "pw\n", "hosts", "approve", id)
	if code != 0 || !strings.Contains(out, "registry v2 published: proposal from k16 approved") {
		t.Fatalf("approve: %d %s %s", code, out, errs)
	}
	if readFile(t, repos(mac)) != reg2 || readFile(t, repos(mac)+".prev") != reg1 {
		t.Fatalf("mac's repos.toml after approval:\n%s", readFile(t, repos(mac)))
	}
	waitFor(t, "k16 hears the approval and takes v2", func() bool {
		out, _, _ := k16.run(t, "", "hosts", "replies")
		return strings.Contains(out, "approved: registry v2") && registryVersion(k16) == 2
	})
	waitFor(t, "k16's presence lists nixrepo", func() bool {
		out, _, _ := mac.run(t, "", "hosts", "--json")
		return strings.Count(out, `"nixrepo"`) >= 2
	})

	// remove --checkouts: a dry run asks nobody; k16's dirty checkout is
	// refused, and the registry keeps the row.
	out, _, code = mac.run(t, "", "repos", "remove", "nixrepo", "--checkouts")
	if code != 0 || !strings.Contains(out, "k16: would be asked to nix nixrepo") || !strings.Contains(out, "would publish registry v3 without nixrepo") {
		t.Fatalf("dry run: %d %s", code, out)
	}
	if err := os.WriteFile(filepath.Join(dir(k16), "wip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errs, code = mac.run(t, "pw\n", "repos", "remove", "nixrepo", "--checkouts", "--apply", "--wait", "20s")
	if code != 0 || !strings.Contains(out, "mac: removed "+dir(mac)) || !strings.Contains(out, "k16: refused: "+dir(k16)+" has uncommitted or untracked changes") ||
		!strings.Contains(out, "pending: k16;") {
		t.Fatalf("remove with k16 dirty: %d %s %s", code, out, errs)
	}
	if registryVersion(mac) != 2 || dirExists(dir(mac)) || !dirExists(dir(k16)) {
		t.Fatalf("after a refusal: registry v%d, mac %v, k16 %v", registryVersion(mac), dirExists(dir(mac)), dirExists(dir(k16)))
	}

	// k16 asleep: the re-ask (k16 only) waits in its inbox.
	os.Remove(filepath.Join(dir(k16), "wip.txt"))
	stopK16()
	out, _, _ = mac.run(t, "pw\n", "repos", "remove", "nixrepo", "--checkouts", "--apply", "--wait", "1s")
	if !regexp.MustCompile(`k16: (asleep|no answer in 1s), queued until `).MatchString(out) || strings.Contains(out, "mac:") ||
		registryVersion(mac) != 2 {
		t.Fatalf("remove with k16 asleep: %s", out)
	}
	serveBox(t, k16)
	waitFor(t, "k16 nixes its checkout", func() bool { return !dirExists(dir(k16)) })

	// Every box has answered: the next registry goes out without the row.
	out, errs, code = mac.run(t, "pw\n", "repos", "remove", "nixrepo", "--checkouts", "--apply", "--wait", "5s")
	if code != 0 || !strings.Contains(out, "registry v3: nixrepo removed") {
		t.Fatalf("remove after k16 answered: %d %s %s", code, out, errs)
	}
	if got := readFile(t, repos(mac)); got != "# the fleet's registry\n\n" {
		t.Fatalf("mac's repos.toml without the row: %q", got)
	}
	waitFor(t, "k16 takes v3", func() bool { return registryVersion(k16) == 3 && !strings.Contains(readFile(t, repos(k16)), "nixrepo") })
}

func TestReposRemoveWithHostsOffEditsTheLocalFile(t *testing.T) {
	setup(t)
	p := filepath.Join(filepath.Dir(os.Getenv("BMESH_CONFIG")), "repos.toml")
	if err := os.WriteFile(p, []byte("[repos.a]\npath = \"/x/a\"\n\n# b\n[repos.b]\npath = \"/x/b\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errs, code := runCLI(t, nil, "repos", "remove", "a", "--checkouts", "--apply"); code != exitRefused || !strings.Contains(errs, "needs [hosts] enabled") {
		t.Fatalf("--checkouts with hosts off: %d %s", code, errs)
	}
	out, _, code := runCLI(t, nil, "repos", "remove", "a")
	if code != 0 || !strings.Contains(out, "would remove a") || !strings.Contains(readFile(t, p), "[repos.a]") {
		t.Fatalf("dry run: %d %s", code, out)
	}
	out, _, code = runCLI(t, nil, "repos", "remove", "a", "--apply")
	if code != 0 || readFile(t, p) != "# b\n[repos.b]\npath = \"/x/b\"\n" {
		t.Fatalf("apply: %d %s\n%s", code, out, readFile(t, p))
	}
	if _, errs, code := runCLI(t, nil, "repos", "publish"); code != exitRefused || !strings.Contains(errs, "hosts are off") {
		t.Fatalf("publish with hosts off: %d %s", code, errs)
	}
}
