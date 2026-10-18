package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// msysOff is the pair of variables that switches the MSYS argument rewriting
// off. docs/STAND.md tells every strand to export them, because a worktree
// outside the repository needs them for git, so this is not an exotic
// environment: it is the one the release gate actually runs in.
var msysOff = []string{"MSYS_NO_PATHCONV=1", "MSYS2_ARG_CONV_EXCL=*"}

// TestPackAndSignSurviveArgumentConversionOff is the case this file exists for,
// and it was written after a measurement, not a guess.
//
// WHAT WENT WRONG. On 18.10.2026 all eleven cases of this package were red on
// origin/main, every one of them with
//
//	/tmp/tmp.XXXXXXXX/ngc-web-all.tar: No such file or directory
//	pack-release: the checksum could not be built
//
// for a file that WAS there - a probe right before the call showed 40960 bytes
// under exactly that name. THE CAUSE: openssl on this machine is a native
// windows binary (/mingw64/bin/openssl), so it never sees an MSYS path like
// /tmp/tmp.XXXX itself; the MSYS layer rewrites the argument when the process
// starts. With the two variables above exported that rewriting is off, openssl
// gets the string and cannot open it. The same thing hit sign-release.sh one
// call earlier, where it read as "the key could not be read as an Ed25519 key"
// - a sentence about the key, while the broken argument was the -out path.
//
// WHY THE OTHER CASES DO NOT COVER IT. They all inherit os.Environ(), so they
// pass or fail with whatever the shell that started go test happened to export.
// That is why this failure looked like a race between parallel packages for a
// day: run alone from a plain shell the package was green, run from a shell
// with the worktree guard exported it was red. This case puts the variables
// into the CHILD environment itself, so the guard holds whatever the parent
// does.
//
// WHAT IT IS WORTH ON LINUX: nothing is rewritten there and no cygpath exists,
// so the two variables mean nothing and this case repeats its neighbours. It is
// not skipped there anyway, because a case that only exists on one platform is
// a case nobody reads.
func TestPackAndSignSurviveArgumentConversionOff(t *testing.T) {
	needTools(t)

	// The measurement checks its own preparation first: if the variables do not
	// reach the child, this case would pass without ever testing anything.
	probe := exec.Command("sh", "-c", `printf '%s|%s' "${MSYS_NO_PATHCONV-}" "${MSYS2_ARG_CONV_EXCL-}"`)
	probe.Env = append(append([]string{}, os.Environ()...), msysOff...)
	got, err := probe.CombinedOutput()
	if err != nil || string(got) != "1|*" {
		t.Fatalf("the child does not carry the variables: %q, %v", got, err)
	}

	repo := webFixture(t, nil)
	out := filepath.Join(t.TempDir(), "out")
	cmd := exec.Command("sh", filepath.Join(repoRoot(t), "tools", "pack-release.sh"),
		"main", "3", out)
	cmd.Dir = repo
	cmd.Env = append(append(gitEnv(t), "TZ=UTC", "LC_ALL=C",
		"GIT_CONFIG_VALUE_0="+filepath.ToSlash(repo)), msysOff...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pack-release.sh with the rewriting off: %v\n%s", err, b)
	}
	// The archive AND the payload, because the checksum sits between them: a
	// pack that wrote the tar and died at openssl leaves the first and not the
	// second, and that is exactly the failure this case is about.
	for _, name := range []string{"ngc-web-all.tar", "release.payload.json"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	// The signer runs on the same output, with the same variables, because its
	// openssl calls carry paths of their own.
	key := newTestKey(t)
	sign := exec.Command("sh", filepath.Join(repoRoot(t), "tools", "sign-release.sh"), out, key)
	sign.Env = append(append([]string{}, os.Environ()...), append(msysOff, "LC_ALL=C")...)
	b, err := sign.CombinedOutput()
	// Without jq the signer cannot run its self-check and says so; that is a
	// different wall and not this one, so only that one sentence is let through.
	if err != nil && !strings.Contains(string(b), "jq") {
		t.Fatalf("sign-release.sh with the rewriting off: %v\n%s", err, b)
	}
}
