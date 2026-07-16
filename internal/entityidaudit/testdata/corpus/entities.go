package corpus

var _ = EntityState{ID: "acme.ops.robotics.gcs.drone.001"}
var _ = Workflow{EntityIDPattern: "acme.ops.robotics.*.drone.*"}
var _ = EntityQuery{EntityIDPrefix: "acme.ops.robotics"}

var _ = Port{Subject: "raw.sensor.>"}

// entity-id-audit:classify unrelated-glob "raw.sensor.>" line=7 column=23 surface=go-unrelated-glob NATS subscription filter
