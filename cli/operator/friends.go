package operator

// friends.go runs the node in friends mode: a distributed KMS (package kms) instead of staking.
// It keeps the operator identity, database, p2p network, message validation and QBFT, and drops
// everything tied to the beacon chain (beacon client, duties, validator metadata, doppelganger,
// fee recipients) and to the SSV registry contract.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/libp2p/go-libp2p/core/host"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	globalcfg "github.com/ssvlabs/ssv/cli/config"
	ssv_identity "github.com/ssvlabs/ssv/identity"
	"github.com/ssvlabs/ssv/kms"
	"github.com/ssvlabs/ssv/kms/chains"
	"github.com/ssvlabs/ssv/message/signatureverifier"
	"github.com/ssvlabs/ssv/message/validation"
	networkcommons "github.com/ssvlabs/ssv/network/commons"
	p2pv1 "github.com/ssvlabs/ssv/network/p2p"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// FriendsOptions configures the dKMS.
type FriendsOptions struct {
	NetworkName   string `yaml:"NetworkName" env:"FRIENDS_NETWORK" env-description:"Friends network name (separates p2p topics)"`
	OperatorsFile string `yaml:"OperatorsFile" env:"FRIENDS_OPERATORS_FILE" env-description:"JSON file listing the network's operators: [{id, public_key}]"`
	ExecutionAddr string `yaml:"ExecutionAddr" env:"FRIENDS_ETH_RPC" env-description:"Execution client URL of the policy chain (policy reads; also signs for this chain)"`
	ChainID       uint64 `yaml:"ChainID" env:"FRIENDS_CHAIN_ID" env-description:"Chain ID of the policy chain (1: Ethereum mainnet)"`
	// Chains are more chains keys sign for, as "<CAIP-2 id>=<RPC URL>" pairs separated by commas,
	// e.g. "eip155:8453=https://base.example,eip155:10=https://op.example". The policy chain
	// (eip155:<ChainID>) is always included, through ExecutionAddr unless listed here.
	Chains         string `yaml:"Chains" env:"FRIENDS_CHAINS" env-description:"Chains to sign for: <CAIP-2 id>=<RPC URL>, comma-separated"`
	PolicyContract string `yaml:"PolicyContract" env:"FRIENDS_POLICY_CONTRACT" env-description:"Address of the SSVKMSPolicy contract"`
	BroadcastTxs   bool   `yaml:"BroadcastTxs" env:"FRIENDS_BROADCAST_TXS" env-description:"Submit signed transactions to the chain"`
	// PolicyConfirmations pins policy and nonce reads this many blocks behind the head: operators
	// whose RPCs lag the proposer's by a block still agree, and a reorg of the newest blocks cannot
	// change a verdict. 0 for a local chain, 2 on mainnet.
	PolicyConfirmations uint64 `yaml:"PolicyConfirmations" env:"FRIENDS_POLICY_CONFIRMATIONS" env-description:"Blocks behind the head that policy checks read at"`
	// CORSOrigins lets web apps call the signing API directly (e.g. an owner withdrawing while the
	// app's backend is down). Comma-separated origins, "*" for any; empty disables CORS.
	CORSOrigins string `yaml:"CORSOrigins" env:"FRIENDS_CORS_ORIGINS" env-description:"Origins allowed to call the dKMS signing API from a browser"`
}

type friendsConfig struct {
	globalcfg.Global   `yaml:"global"`
	DBOptions          basedb.Options `yaml:"db"`
	P2pNetworkConfig   p2pv1.Config   `yaml:"p2p"`
	KeyStore           KeyStore       `yaml:"KeyStore"`
	OperatorPrivateKey string         `yaml:"OperatorPrivateKey" env:"OPERATOR_KEY" env-description:"Operator private key (base64 PEM)"`
	NetworkPrivateKey  string         `yaml:"NetworkPrivateKey" env:"NETWORK_PRIVATE_KEY" env-description:"Private key for P2P network identity"`
	SSVAPIAddress      string         `yaml:"SSVAPIAddress" env:"SSV_API_ADDRESS" env-description:"Listen address for the API"`
	SSVAPIPort         int            `yaml:"SSVAPIPort" env:"SSV_API_PORT" env-description:"Port for the API (dKMS endpoints)"`
	Friends            FriendsOptions `yaml:"friends"`
}

func (c *friendsConfig) ApplyDefaults() {
	c.Global.ApplyDefaults()
	c.DBOptions.ApplyDefaults()
	c.P2pNetworkConfig.ApplyDefaults()
	c.SSVAPIPort = 16000
	c.Friends = FriendsOptions{NetworkName: "friends-local", ChainID: 1, BroadcastTxs: true}
}

// friendsOperator is an entry of the operators file.
type friendsOperator struct {
	ID     spectypes.OperatorID `json:"id"`
	PubKey string               `json:"public_key"`
}

func runFriendsNode(ctx context.Context, logger *zap.Logger, cfg *friendsConfig) error {
	netCfg := networkconfig.FriendsNetwork(cfg.Friends.NetworkName)
	logger.Info("🤝 starting in friends mode (distributed KMS, no beacon chain)",
		zap.String("network", netCfg.SSV.Name), zap.Uint64("chain_id", cfg.Friends.ChainID), zap.String("policy", cfg.Friends.PolicyContract))

	// Operator identity.
	var privKey keys.OperatorPrivateKey
	var privKeyPEM string
	switch {
	case cfg.KeyStore.PrivateKeyFile != "":
		pk, decrypted, err := privateKeyFromKeystore(cfg.KeyStore.PrivateKeyFile, cfg.KeyStore.PasswordFile)
		if err != nil {
			return fmt.Errorf("could not open operator keystore: %w", err)
		}
		privKey, privKeyPEM = pk, base64.StdEncoding.EncodeToString(decrypted)
	case cfg.OperatorPrivateKey != "":
		pk, err := keys.PrivateKeyFromString(cfg.OperatorPrivateKey)
		if err != nil {
			return fmt.Errorf("could not decode operator private key: %w", err)
		}
		privKey, privKeyPEM = pk, cfg.OperatorPrivateKey
	default:
		return errors.New("no operator key: set KeyStore.PrivateKeyFile or OperatorPrivateKey")
	}
	pubKeyB64, err := privKey.Public().Base64()
	if err != nil {
		return err
	}

	// Database and the operator registry (static in friends mode).
	cfg.DBOptions.Ctx = ctx
	db, err := badger.New(logger, cfg.DBOptions)
	if err != nil {
		return fmt.Errorf("could not open db: %w", err)
	}
	defer db.Close()
	nodeStorage, err := operatorstorage.NewNodeStorage(netCfg.Beacon, logger, db)
	if err != nil {
		return err
	}
	if err := ensureOperatorPrivateKey(nodeStorage, privKey, privKeyPEM); err != nil {
		return err
	}
	operatorIDs, err := loadFriendsOperators(nodeStorage, cfg.Friends.OperatorsFile)
	if err != nil {
		return err
	}
	operatorDataStore, err := setupOperatorDataStore(nodeStorage, pubKeyB64)
	if err != nil {
		return err
	}
	if !operatorDataStore.OperatorIDReady() {
		return errors.New("this operator's public key is not in the operators file")
	}
	operatorID := operatorDataStore.GetOperatorID()
	logger = logger.With(zap.Uint64("operator_id", operatorID))
	signer := ssvtypes.NewSsvOperatorSigner(privKey, operatorDataStore.GetOperatorID)

	// The chain keys sign for, and its policy contract.
	if !common.IsHexAddress(cfg.Friends.PolicyContract) {
		return fmt.Errorf("invalid policy contract address %q", cfg.Friends.PolicyContract)
	}
	// After a deploy every operator starts at once: spread their first chain reads, so they don't
	// hit a hosted RPC provider's rate limit together.
	if kms.HostedRPC(cfg.Friends.ExecutionAddr) {
		stagger := time.Duration((operatorID-1)%8)*2*time.Second + time.Duration(rand.Float64()*float64(2*time.Second))
		logger.Info("staggering startup chain reads", zap.Duration("wait", stagger))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(stagger):
		}
	}
	kms.StartRPCStats(ctx, logger)
	chain, err := connectChain(ctx, logger, cfg.Friends)
	if err != nil {
		return err
	}

	executors, err := chainExecutors(cfg.Friends)
	if err != nil {
		return err
	}
	svc, err := kms.New(kms.Options{
		Logger:        logger,
		DB:            db,
		NetworkConfig: netCfg,
		OperatorID:    operatorDataStore.GetOperatorID,
		Signer:        signer,
		OperatorKey:   privKey,
		OperatorKeys: func(id spectypes.OperatorID) (string, error) {
			od, found, err := nodeStorage.GetOperatorData(nil, id)
			if err != nil {
				return "", err
			}
			if !found {
				return "", errors.New("operator not registered")
			}
			return od.PublicKey, nil
		},
		PolicyChain:    chain,
		Executors:      executors,
		PolicyContract: common.HexToAddress(cfg.Friends.PolicyContract),
		BroadcastTxs:   cfg.Friends.BroadcastTxs,
		Confirmations:  cfg.Friends.PolicyConfirmations,
	})
	if err != nil {
		return err
	}

	// P2P: the node's network stack, validating dKMS messages against the key registry.
	messageValidator := validation.New(
		netCfg,
		nodeStorage.ValidatorStore(),
		nodeStorage,
		dutystore.New(),
		signatureverifier.NewSignatureVerifier(nodeStorage),
		validation.WithLogger(logger),
		validation.WithKMS(svc),
	)
	_, unprotect, err := decideNetworkKeyProtectors(ctx, logger, db, false, privKey, nil)
	if err != nil {
		return err
	}
	netKey, err := ssv_identity.NewIdentityStore(logger, db, unprotect).SetupNetworkKey(ctx, cfg.NetworkPrivateKey)
	if err != nil {
		return fmt.Errorf("could not set up network key: %w", err)
	}
	p2pCfg := &cfg.P2pNetworkConfig
	if p2pCfg.Subnets == "" {
		// Join the committee topic of the whole operator set from the start: peers that share no
		// subnet are dropped as irrelevant, and keys (which add their committee's topic) come later.
		var subnets networkcommons.Subnets
		subnets.Set(networkcommons.BooleCommitteeSubnet(operatorIDs))
		p2pCfg.Subnets = subnets.StringHex()
	}
	p2pCfg.Ctx = ctx
	p2pCfg.NetworkPrivateKey = netKey
	p2pCfg.NodeStorage = nodeStorage
	p2pCfg.OperatorDataStore = operatorDataStore
	p2pCfg.NetworkConfig = netCfg
	p2pCfg.MessageValidator = messageValidator
	p2pCfg.GetValidatorStats = func() (uint64, uint64, uint64, error) { return 0, 0, 0, nil }
	p2pNetwork, err := p2pv1.New(logger, p2pCfg)
	if err != nil {
		return fmt.Errorf("could not create p2p network: %w", err)
	}
	defer p2pNetwork.Close()
	p2pNetwork.UseMessageRouter(svc)
	if err := p2pNetwork.Setup(); err != nil {
		return fmt.Errorf("could not set up p2p network: %w", err)
	}
	if err := p2pNetwork.Start(); err != nil {
		return fmt.Errorf("could not start p2p network: %w", err)
	}
	svc.SetNetwork(p2pNetwork)
	// Keeps the node's subnet view current (fixed subnets + keys' committee topics); without it
	// the node considers every peer irrelevant and trims it.
	go p2pNetwork.UpdateSubnets()
	go keepTrustedPeers(ctx, logger, p2pNetwork.Host(), p2pCfg.TrustedPeers)
	if err := svc.Start(ctx); err != nil {
		return err
	}

	// API.
	router := chi.NewRouter()
	router.Use(corsMiddleware(cfg.Friends.CORSOrigins))
	router.Get("/v1/node/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"operator_id": operatorID, "peers": len(p2pNetwork.Host().Network().Peers())})
	})
	svc.Routes(router)
	srv := &http.Server{
		Addr:              net.JoinHostPort(cfg.SSVAPIAddress, strconv.Itoa(cfg.SSVAPIPort)),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	logger.Info("dKMS API listening", zap.String("addr", srv.Addr))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// loadFriendsOperators saves the operators file into the node's operator registry and returns
// the operator IDs, sorted.
func loadFriendsOperators(nodeStorage operatorstorage.Storage, path string) ([]spectypes.OperatorID, error) {
	if path == "" {
		return nil, errors.New("FRIENDS_OPERATORS_FILE is required")
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("could not read operators file: %w", err)
	}
	var ops []friendsOperator
	if err := json.Unmarshal(data, &ops); err != nil {
		return nil, fmt.Errorf("could not parse operators file: %w", err)
	}
	ids := make([]spectypes.OperatorID, 0, len(ops))
	for _, op := range ops {
		if _, err := keys.PublicKeyFromString(op.PubKey); err != nil {
			return nil, fmt.Errorf("operator %d public key: %w", op.ID, err)
		}
		if _, err := nodeStorage.SaveOperatorData(nil, &registrystorage.OperatorData{ID: op.ID, PublicKey: op.PubKey}); err != nil {
			return nil, err
		}
		ids = append(ids, op.ID)
	}
	slices.Sort(ids)
	return ids, nil
}

// keepTrustedPeers redials the configured trusted peers whenever they disconnect. A friends
// network is a small fixed set of operators; p2p only dials trusted peers once at startup.
func keepTrustedPeers(ctx context.Context, logger *zap.Logger, h host.Host, trusted []string) {
	var peers []peer.AddrInfo
	for _, entry := range trusted {
		for addr := range strings.SplitSeq(entry, ",") {
			if ai, err := peer.AddrInfoFromString(strings.TrimSpace(addr)); err == nil {
				peers = append(peers, *ai)
			}
		}
	}
	if len(peers) == 0 {
		return
	}
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		for _, ai := range peers {
			if h.Network().Connectedness(ai.ID) == libp2pnetwork.Connected {
				continue
			}
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := h.Connect(cctx, ai); err != nil {
				logger.Debug("could not connect to trusted peer", zap.String("peer", ai.ID.String()), zap.Error(err))
			}
			cancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// connectChain waits for the execution client and the policy contract (in a local setup they may
// still be starting).
func connectChain(ctx context.Context, logger *zap.Logger, opts FriendsOptions) (*kms.EthChain, error) {
	for attempt := 0; ; attempt++ {
		chain, err := kms.NewEthChain(ctx, opts.ExecutionAddr, opts.ChainID, common.HexToAddress(opts.PolicyContract))
		if err == nil {
			return chain, nil
		}
		// The URL may carry an RPC provider's API key: keep it out of the logs.
		logger.Info("waiting for the execution client and policy contract", zap.String("error", strings.ReplaceAll(err.Error(), opts.ExecutionAddr, redactURL(opts.ExecutionAddr))))
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(connectBackoff(attempt)):
		}
	}
}

// connectBackoff is the wait before connect attempt n+1: exponential with jitter, from 1.5-3s up to
// 15-30s.
func connectBackoff(n int) time.Duration {
	ceil := min(30*time.Second, 3*time.Second<<min(n, 4))
	return ceil/2 + time.Duration(rand.Float64()*float64(ceil/2))
}

// redactURL keeps only an RPC URL's scheme and host.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<rpc>"
	}
	return u.Scheme + "://" + u.Host + "/…"
}

// corsMiddleware opens the read and signing endpoints to browsers from the allowed origins. Signing
// requests carry their own authorization (the requester's signature); loading keys stays closed.
func corsMiddleware(origins string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range strings.Split(origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			allowed[o] = true
		}
	}
	open := func(r *http.Request) bool {
		if r.URL.Path == "/v1/kms/sign" || strings.HasPrefix(r.URL.Path, "/v1/kms/requests/") {
			return true
		}
		return r.URL.Path != "/v1/kms/keys" && (r.Method == http.MethodGet || r.Method == http.MethodOptions)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (allowed["*"] || allowed[origin]) && open(r) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// chainExecutors builds an executor for every chain the node signs for. Executors dial lazily: a
// chain whose RPC is down fails only its own requests.
func chainExecutors(opts FriendsOptions) (map[string]kms.Executor, error) {
	rpcs := map[string]string{fmt.Sprintf("eip155:%d", opts.ChainID): opts.ExecutionAddr}
	for _, entry := range strings.Split(opts.Chains, ",") {
		if entry = strings.TrimSpace(entry); entry == "" {
			continue
		}
		chain, url, ok := strings.Cut(entry, "=")
		if !ok || url == "" {
			return nil, fmt.Errorf("chains: %q is not <CAIP-2 id>=<RPC URL>", entry)
		}
		rpcs[strings.TrimSpace(chain)] = strings.TrimSpace(url)
	}
	out := map[string]kms.Executor{}
	for chain, url := range rpcs {
		id, _, err := chains.ParseID(chain)
		if err != nil {
			return nil, fmt.Errorf("chains: %w", err)
		}
		switch id.Namespace {
		case "eip155":
			chainID, _ := strconv.ParseUint(id.Reference, 10, 64)
			out[id.String()] = kms.NewEVMExecutor(url, chainID)
		default:
			return nil, fmt.Errorf("chains: no executor for %s chains yet", id.Namespace)
		}
	}
	return out, nil
}
