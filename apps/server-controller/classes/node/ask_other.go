//go:build !linux

package node

import "os"

// These two answer for the systems the controller does not run on. Asking a
// terminal what it is and switching its echo off are the calls of Linux, and
// NGC is built for Linux alone: nothing counts as a terminal here, so the code
// is taken from the standard input the way a script hands it over. The file is
// here so that the tests of this package also run where they are written.
func isTerminal(*os.File) bool { return false }

func hideInput(*os.File) (restore func() bool, ok bool) { return nil, false }
