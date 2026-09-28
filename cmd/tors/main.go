// Command tors is the standard distribution: the core with every module
// maintained in this repository.
package main

import (
	torscmd "github.com/tori43-hash/tors/cmd"

	_ "github.com/tori43-hash/tors/modules/standard"
)

func main() { torscmd.Main() }
