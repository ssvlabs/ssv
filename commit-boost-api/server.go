package cbapi

import (
	"encoding/json"
	"fmt"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/fasthttp/router"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
	"time"
)

// Server implements commit-boost singer API described here:
// https://commit-boost.github.io/commit-boost-client/api/
type Server struct {
	logger    *zap.Logger
	router    *router.Router
	vProvider validatorProvider
}

func New(
	logger *zap.Logger,
	vProvider validatorProvider,
) *Server {
	r := router.New()

	server := &Server{
		logger:    logger.Named("commit-boost API"),
		router:    r,
		vProvider: vProvider,
	}

	r.GET("/signer/v1/get_pubkeys", server.handleGetPubkeys)
	r.POST("/signer/v1/request_signature", server.handleRequestSignature)
	// TODO generate_proxy_key is not implemented at the moment
	//r.POST("/signer/v1/generate_proxy_key", server.handleListValidators)

	r.GET("/status", server.handleStatus)

	return server
}

func (s *Server) Run(addr string) error {
	s.logger.Info("starting listening for connections", zap.String("addr", addr))

	err := fasthttp.ListenAndServe(addr, s.router.Handler)
	if err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}
	return nil
}

func (s *Server) handleGetPubkeys(ctx *fasthttp.RequestCtx) {
	resp := GetPubKeysResponse{}

	pubkeys := s.vProvider.ListValidatorPubKeys()
	for _, pubkey := range pubkeys {
		resp.Keys = append(resp.Keys, ValidatorKeys{
			Consensus:  pubkey.String(),
			ProxyBLS:   nil, // we don't support these at the moment
			ProxyECDSA: nil, // we don't support these at the moment
		})
	}

	s.writeJSON(ctx, fasthttp.StatusOK, resp)
}

func (s *Server) handleRequestSignature(ctx *fasthttp.RequestCtx) {
	body := ctx.PostBody()
	if len(body) == 0 {
		s.writeJSONErr(ctx, fasthttp.StatusBadRequest, fmt.Errorf("request body is empty"))
		return
	}
	var req RequestSignatureRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeJSONErr(ctx, fasthttp.StatusBadRequest, fmt.Errorf("failed to parse request: %w", err))
		return
	}
	if req.ValidatorKeyType != "consensus" {
		err := fmt.Errorf("only consensus validator key type is supported at the moment, got: %s", req.ValidatorKeyType)
		s.writeJSONErr(ctx, fasthttp.StatusBadRequest, err)
		return
	}
	pubkeyDecoded, err := hexutil.Decode(req.ValidatorPubKeyHex)
	if err != nil {
		err = fmt.Errorf("decode pubkey: %s, %w", req.ValidatorPubKeyHex, err)
		s.writeJSONErr(ctx, fasthttp.StatusBadRequest, err)
		return
	}
	if len(pubkeyDecoded) != 48 {
		// TODO - not sure if that's true for proxy keys - need to check that
		err = fmt.Errorf("pubkey size must be 48 bytes, got %d instead (pubkey: %s)", len(pubkeyDecoded), req.ValidatorPubKeyHex)
		s.writeJSONErr(ctx, fasthttp.StatusBadRequest, err)
		return
	}

	v, ok := s.vProvider.GetValidator(spectypes.ValidatorPK(pubkeyDecoded))
	if !ok {
		err := fmt.Errorf("validator is not found for pubkey: %s", req.ValidatorPubKeyHex)
		s.writeJSONErr(ctx, fasthttp.StatusNotFound, err)
		return
	}
	dutyRunner := v.GetPreconfCommitmentRunner()
	validatorIdx := v.Share.ValidatorIndex

	resultCh, err := dutyRunner.StartNewDutyWithResponse(ctx, s.logger, validatorIdx, req.ObjectRootHex)
	if err != nil {
		err = fmt.Errorf("start preconf-commitment duty: %w", err)
		s.writeJSONErr(ctx, fasthttp.StatusInternalServerError, err)
		return
	}

	select {
	case result := <-resultCh:
		commitmentSigHex := hexutil.Encode(result.CommitmentSignature)
		s.writeJSON(ctx, fasthttp.StatusOK, commitmentSigHex)
		return
	case <-time.After(60 * time.Second): // TODO - what timeout should we use ?
		err = fmt.Errorf("timed out waiting for preconf-commitment duty to finish")
		s.writeJSONErr(ctx, fasthttp.StatusInternalServerError, err)
		return
	case <-ctx.Done():
		err = fmt.Errorf("request ctx terminated waiting for preconf-commitment duty to finish")
		s.writeJSONErr(ctx, fasthttp.StatusInternalServerError, err)
		return
	}
}

func (s *Server) handleStatus(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	s.writeString(ctx, "OK")
}

func (s *Server) writeString(ctx *fasthttp.RequestCtx, str string) {
	if _, writeErr := ctx.WriteString(str); writeErr != nil {
		s.logger.Error("failed to write response", zap.Error(writeErr))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	}
}

func (s *Server) writeJSON(ctx *fasthttp.RequestCtx, statusCode int, resp any) {
	ctx.SetContentType("application/json")
	respJSON, err := json.Marshal(resp)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		s.writeString(ctx, fmt.Sprintf("failed to marshal response: %v", err))
		return
	}
	ctx.SetStatusCode(statusCode)
	if _, writeErr := ctx.Write(respJSON); writeErr != nil {
		s.logger.Error("failed to write response", zap.Error(writeErr))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	}
}

func (s *Server) writeJSONErr(ctx *fasthttp.RequestCtx, statusCode int, err error) {
	ctx.SetContentType("application/json")
	errResp := ErrorResponse{
		Code:    statusCode,
		Message: err.Error(),
	}
	errRespJSON, err := json.Marshal(errResp)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		s.writeString(ctx, fmt.Sprintf("failed to marshal error response: %v", err))
		return
	}
	ctx.SetStatusCode(statusCode)
	if _, writeErr := ctx.Write(errRespJSON); writeErr != nil {
		s.logger.Error("failed to write response", zap.Error(writeErr))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	}
}

type validatorProvider interface {
	ListValidatorPubKeys() []phase0.BLSPubKey
	GetValidator(pubKey spectypes.ValidatorPK) (*validator.Validator, bool)
}
