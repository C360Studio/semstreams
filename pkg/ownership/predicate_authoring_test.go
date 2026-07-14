package ownership

import "github.com/c360studio/semstreams/vocabulary"

func init() {
	for _, predicate := range []string{
		"a.b.c",
		"inv.of.it",
		"sensorml.component.is-hosted-by",
		"sensorml.process.label",
		"sensorml.system.hosts",
		"test.edge.p",
		"test.value.p",
	} {
		vocabulary.Register(predicate)
	}
}
