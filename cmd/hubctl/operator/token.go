// Package operator signs short-lived operator delegations for hubctl commands.
package operator

import (
	"encoding/json"
	"fmt"
	"time"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/shared/auth"
)

func Token(owner, space, target, audience, keyPath string) (string, error) {
	key, err := auth.LoadPrivateKey(keyPath)
	if err != nil {
		return "", fmt.Errorf("operator key: %w", err)
	}
	claims, err := json.Marshal(idv1.OwnerClaims{
		Sub: "operator", Owner: owner, Space: space, Target: target,
		Aud: []string{audience}, Exp: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("operator claims: %w", err)
	}
	tok, err := auth.Sign(key, "operator", claims)
	if err != nil {
		return "", fmt.Errorf("operator sign: %w", err)
	}
	return tok, nil
}
