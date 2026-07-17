package ownership

import "github.com/c360studio/semstreams/vocabulary"

func init() {
	vocabulary.Register("a.b.c")
	vocabulary.Register("inv.of.it")
	vocabulary.Register("sensorml.component.is-hosted-by")
	vocabulary.Register("sensorml.process.label")
	vocabulary.Register("sensorml.system.hosts")
	vocabulary.Register("test.edge.claimed")
	vocabulary.Register("test.edge.shared")
	vocabulary.Register("test.edge.p")
	vocabulary.Register("test.value.a")
	vocabulary.Register("test.value.b")
	vocabulary.Register("test.value.p")
	vocabulary.Register("web.relation.backlink")
}
