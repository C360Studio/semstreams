# Manifest shape assessment

Independent reviewer cli_review endorsed advice SHA-256
`499401dc98495c1ead31503582dae0b9a1d29dfdd07e8b360c2d447755fda22d` against question
`37fc6ea1aca7da76e21eaf3121f35f7b0b4969c1f887f735ecef5ffdd6506248`.
The reviewer found no material contradiction with the accepted design/handoff. The specific WriteManifest operation,
shared ArtifactReference value, initialized snapshot, Writer-derived destination/digest, immutable retention and
failed-manifest terminal-write attempt preserve existing ownership and lifetime. Reporter owns manifest meaning;
live child verification checks retained bytes/digests; historical LoadRun remains path-independent.
This is shape advice only. No source edits, tests, concurrent Writer code approval or completed producer proof.

The reviewer then required an explicit uniform Path convention, recommending absolute paths or an equally explicit
uniform relative-base rule. Root selected the latter, matching the architect advice and current VerifyChild:
ArtifactReference.Path is relative to the emitting Writer's resolved absolute output directory. Both methods use
this convention. The reporter resolves it from its initialized run's Environment.output_dir. An embedded child
manifest/log reference is instead resolved using that child's own Environment.output_dir when verifying its bytes.
Normalize Writer construction/input paths before deriving the relative reference; retain a relative-output-directory
boundary test. No new path abstraction, caller knob, or compatibility alias is added.

Root reconciled this representation within the already accepted scope and released the new export for implementation.
Independent implementation review remains required. No new owner scope ruling is implied.
