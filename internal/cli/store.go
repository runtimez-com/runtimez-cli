package cli

import (
	"errors"

	"github.com/runtimez-com/runtimez-cli/internal/auth"
)

func deleteCredentials(ref string) error {
	err := auth.Open().Delete(ref)
	if errors.Is(err, auth.ErrNotFound) {
		return nil
	}
	return err
}
