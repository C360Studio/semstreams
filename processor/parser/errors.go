package parser

import "errors"

// Common parsing errors
var (
	ErrInvalidFormat = errors.New("invalid data format")
	ErrEmptyData     = errors.New("empty data")
	ErrParsingFailed = errors.New("parsing failed")
)
