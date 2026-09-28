// Package standard imports every module maintained in this repository.
package standard

import (
	_ "github.com/tori43-hash/tors/modules/billing"
	_ "github.com/tori43-hash/tors/modules/billing/stars"
	_ "github.com/tori43-hash/tors/modules/catalog"
	_ "github.com/tori43-hash/tors/modules/database"
	_ "github.com/tori43-hash/tors/modules/httpserver"
	_ "github.com/tori43-hash/tors/modules/jobs"
	_ "github.com/tori43-hash/tors/modules/kv"
	_ "github.com/tori43-hash/tors/modules/kv/memory"
	_ "github.com/tori43-hash/tors/modules/kv/postgres"
	_ "github.com/tori43-hash/tors/modules/panels"
	_ "github.com/tori43-hash/tors/modules/panels/fake"
	_ "github.com/tori43-hash/tors/modules/panels/remnawave"
	_ "github.com/tori43-hash/tors/modules/subscriptions"
	_ "github.com/tori43-hash/tors/modules/support"
	_ "github.com/tori43-hash/tors/modules/telegram"
	_ "github.com/tori43-hash/tors/modules/trial"
	_ "github.com/tori43-hash/tors/modules/ui"
	_ "github.com/tori43-hash/tors/modules/users"
	_ "github.com/tori43-hash/tors/modules/yamladapter"
)
