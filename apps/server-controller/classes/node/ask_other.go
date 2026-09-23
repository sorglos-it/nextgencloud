//go:build !linux

package node

import "os"

// hideInput answers for the systems the controller does not run on: only the
// Linux file switches the echo of a terminal off. NGC is built for Linux
// alone; this file is here so that the tests of this package also run where
// they are written.
func hideInput(*os.File) (restore func(), ok bool) { return nil, false }
