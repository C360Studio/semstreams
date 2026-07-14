package projection

import "github.com/c360studio/semstreams/vocabulary"

func init() {
	for _, predicate := range []string{
		"a.b.c",
		"cs-api.deployment.deployed-systems",
		"cs-api.deployment.parent",
		"duplicate.value.predicate",
		"sensorml.component.is-hosted-by",
		"sensorml.process.description",
		"sensorml.process.label",
		"sensorml.process.position",
		"sensorml.process.type",
		"sensorml.process.uid",
		"shared.value.p",
		"test.value.p",
	} {
		vocabulary.Register(predicate)
	}
}
