package provider

import "errors"

var (
	ErrStreamNotSupported = errors.New("streaming not supported")
	ErrInvalidToken       = errors.New("invalid token")
	ErrRequestFailed      = errors.New("request failed")
	ErrUnknownProvider    = errors.New("unknown provider")
)
