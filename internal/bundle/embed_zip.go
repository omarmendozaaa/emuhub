//go:build bundle && bundlezip

package bundle

import _ "embed"

// The complete builds carry the emulators themselves, for installing
// without downloading them.
//
//go:embed data/emulators.zip
var bundledZip []byte

func init() { embeddedZip = bundledZip }
