package projection

import "github.com/c360studio/semstreams/vocabulary"

func init() {
	vocabulary.Register("a.b.c")
	vocabulary.Register("cs-api.deployment.deployed-systems")
	vocabulary.Register("cs-api.deployment.parent")
	vocabulary.Register("duplicate.value.predicate")
	vocabulary.Register("sensorml.component.is-hosted-by")
	vocabulary.Register("sensorml.process.description")
	vocabulary.Register("sensorml.process.label")
	vocabulary.Register("sensorml.process.position")
	vocabulary.Register("sensorml.process.type")
	vocabulary.Register("sensorml.process.uid")
	vocabulary.Register("shared.value.p")
	vocabulary.Register("test.value.p")
}
