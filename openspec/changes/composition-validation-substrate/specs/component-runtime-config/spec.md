## REMOVED Requirements

### Requirement: Explicit Flow publication reports persistence without activation

**Reason**: The explicit Flow publication operation (`POST /flowbuilder/flows/{id}/publish-component-configs`,
`service/flow_service.go:463-536`) is removed under ADR-100 together with the saved-diagram surface. The
activation boundary it described — a `components.*` write is desired state for the next boot and never runtime
activation — is unchanged and remains stated by "Component configuration activates only during process
construction" in this capability.

**Migration**: Edit the product's configuration and restart. No framework verb writes `components.*` desired state
after this change; `config.Manager.PutComponentToKV` remains an internal Config Manager method with no route or tool
in front of it.
