// Package release names the parts of an NGC release, and it names them by
// ASKING THE CONTRACT instead of holding a list of its own.
//
// Why that way round: apps/cli/classes/release.go already holds a written
// list (releaseParts). A second written list in this package would be a
// second place to forget an architecture, and the two would drift apart
// without anything going red. So Parts enumerates candidates and keeps the
// ones store.Release.Validate accepts - the contract is the list, and
// TestTheToolAndTheContractNameTheSameParts holds the tool's written list
// against it.
//
// This package builds nothing and signs nothing. What a release is, who
// signs it and which of its parts is reproducible is written down in
// docs/release-weg.md; the cases beside this file are the ones that can be
// RUN without podman, without a server and without a release key.
package release

import (
	"sort"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// Part is one file of a release: a component and the machine it is for.
type Part struct {
	Name string // store.ReleaseWeb, store.ReleaseAPI, store.ReleaseController
	Arch string // "all", "amd64", "arm64"
}

// File is the name this part carries in a release directory. The extension
// is the one apps/cli/classes/release.go looks for; what the file CONTAINS
// is a decision that is still open for every part (docs/release-weg.md,
// "Zwei offene Entscheidungen").
func (p Part) File() string { return p.Name + "-" + p.Arch + ".tar" }

// candidates are the component names and architecture names a release could
// possibly pair. It is deliberately WIDER than what is allowed: everything
// here is offered to the contract, and the contract refuses what is not a
// part. A name missing here would be invisible, so it lists every component
// the contract knows plus the architectures it spells anywhere.
var candidates = struct {
	names, archs []string
}{
	names: []string{store.ReleaseWeb, store.ReleaseAPI, store.ReleaseController},
	archs: []string{"all", "amd64", "arm64"},
}

// Parts are the part-and-architecture pairs a release may list, in a fixed
// order (by name, then architecture) so that two runs give the same answer.
//
// Each candidate is offered to store.Release.Validate as a release of ONE
// file. A pair the contract refuses is not a part. Nothing else about the
// release is varied, so a refusal can only come from the pair itself: the
// other fields are filled with values Validate accepts (seq 1, a positive
// issue time, a valid version, a relative address, one byte of size, a
// 32-byte checksum) and that is checked once in
// TestTheProbeItselfWouldPass.
func Parts() []Part {
	var out []Part
	for _, n := range candidates.names {
		for _, a := range candidates.archs {
			if probe(Part{Name: n, Arch: a}).Validate() == nil {
				out = append(out, Part{Name: n, Arch: a})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Arch < out[j].Arch
	})
	return out
}

// probe is a one-file release that is valid in every respect EXCEPT possibly
// the part and architecture of p. See Parts.
func probe(p Part) *store.Release {
	return &store.Release{
		Seq:     1,
		Issued:  1,
		Version: "1.0.0",
		Files: []store.File{{
			Part:   p.Name,
			Arch:   p.Arch,
			URL:    p.File(),
			Size:   1,
			SHA256: make([]byte, 32),
		}},
	}
}
