// Package componentadmission gates framework-internal component admission
// coordination across the component and service packages.
package componentadmission

// Access is required by Registry operations that exist only for framework
// lifecycle coordination. The Go internal-package rule prevents downstream
// adopters from importing this capability.
type Access struct{}
