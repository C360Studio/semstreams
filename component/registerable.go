package component

// Registerable allows components to self-describe for registry registration
type Registerable interface {
	Registration() Registration
}
