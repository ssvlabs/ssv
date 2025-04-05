package dirk

import (
	"context"
	"fmt"

	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

// DeleteKeystore removes validator shares from Dirk
func (s *Signer) DeleteKeystore(ctx context.Context, req web3signer.DeleteKeystoreRequest) (web3signer.DeleteKeystoreResponse, error) {
	if err := s.connect(ctx); err != nil {
		return web3signer.DeleteKeystoreResponse{}, err
	}

	responseData := make([]web3signer.KeyManagerResponseData, 0, len(req.Pubkeys))

	for _, pubkey := range req.Pubkeys {
		err := DeleteByPublicKey(ctx, s.client, s.credentials.ConfigDir, pubkey)
		if err != nil {
			responseData = append(responseData, web3signer.KeyManagerResponseData{
				Status:  web3signer.StatusError,
				Message: fmt.Sprintf("failed to delete account: %v", err),
			})
			continue
		}

		responseData = append(responseData, web3signer.KeyManagerResponseData{
			Status:  web3signer.StatusDeleted,
			Message: fmt.Sprintf("deleted account with public key %s (restart Dirk to use)", pubkey.String()),
		})
	}

	return web3signer.DeleteKeystoreResponse{
		Data: responseData,
	}, nil
}
