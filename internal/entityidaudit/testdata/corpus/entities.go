package corpus

var _ = EntityState{ID: "acme.ops.robotics.gcs.drone.001"}
var _ = Workflow{EntityIDPattern: "acme.ops.robotics.*.drone.*"}
var _ = EntityQuery{EntityIDPrefix: "acme.ops.robotics"}

const entityIDPattern = "raw.sensor.>"

// entity-id-audit:classify unrelated-glob "raw.sensor.>" line=7 column=25 surface=go-declaration:entityIDPattern entity_id_pattern_invalid:arity NATS subscription filter

var _ = Contract{EntityPattern: "acme.ops.robotics.*.drone.*"}
