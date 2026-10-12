// Package attack holds the attacks of apps/server-api/CLAUDE.md, section
// "Tests", in one place, so that the list reads together instead of lying
// spread over the packages it attacks.
//
// It carries no code of its own on purpose: every case here drives the API
// that package server builds -- the real router, the real login, the real
// dispatcher, the real relay -- and nothing of this package is ever imported
// by the program.
//
// Every case is built the same way, and that shape is the point of the
// package rather than a habit:
//
//   - One reachability check per attack, which runs the SAME way without the
//     attack and has to get through. Without it a green case would only say
//     that something refused, and an earlier wall refuses most things.
//   - The reachability check names the wall it reached. For a forwarded
//     channel that wall is E_UNREACHABLE: no controller is paired in a test,
//     so a channel that reaches package relay ends there -- which is exactly
//     the proof that signature, task and rights let it through.
//
// What is NOT measurable here is said in the cases themselves, and the list
// has shrunk once already: the actions of ngc.store/api had no handler
// registered in coreapp.New, so every one of them answered E_NOTFOUND
// whatever was sent, and an attack through them measured the missing handler
// and nothing else. They are wired now, and store_test.go attacks them for
// real. What is still out of reach:
//
//   - Nothing in this package pairs a CONTROLLER, so nothing behind a tunnel
//     is measured -- which is the same fact that makes E_UNREACHABLE the wall
//     above.
//   - With it, the second half of the truncation attack: an end-to-end stream
//     that stops without the controller's end. TestTruncatedDataIsAnError
//     says so where it would stand.
package attack
