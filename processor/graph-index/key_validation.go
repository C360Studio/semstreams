package graphindex

import (
	"github.com/c360studio/semstreams/natsclient"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

func validateEntityLiteralKey(entityID string) error {
	if err := semtypes.ValidateEntityID(entityID); err != nil {
		return err
	}
	return natsclient.ValidateKVLiteralKey(entityID)
}

func validateKVPrefix(prefix string) error {
	return natsclient.ValidateKVWildcardFilter(prefix + ">")
}
