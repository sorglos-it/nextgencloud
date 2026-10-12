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
// What is NOT measurable here is said in the cases themselves: the actions of
// ngc.store/api have no handler registered in coreapp.New, so every one of
// them answers E_NOTFOUND whatever is sent. An attack against such an action
// would measure the missing handler and nothing else.
package attack
