// Package standard imports every module maintained in this repository.
package standard

import (
	_ "github.com/tori43-hash/tors/modules/database"
	_ "github.com/tori43-hash/tors/modules/httpserver"
	_ "github.com/tori43-hash/tors/modules/jobs"
	_ "github.com/tori43-hash/tors/modules/kv"
	_ "github.com/tori43-hash/tors/modules/kv/memory"
	_ "github.com/tori43-hash/tors/modules/kv/postgres"
	_ "github.com/tori43-hash/tors/modules/yamladapter"
)
