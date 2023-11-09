package etherman

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"

	common2 "github.com/evm-layer2/selaginella/synchronizer/common"
	"github.com/evm-layer2/selaginella/synchronizer/retry"
)

const (
	defaultDialTimeout    = 5 * time.Second
	defaultDialAttempts   = 5
	defaultRequestTimeout = 10 * time.Second
)

type EthClient interface {
	BlockHeaderByNumber(*big.Int) (*types.Header, error)
	BlockHeaderByHash(common.Hash) (*types.Header, error)
	BlockHeadersByRange(*big.Int, *big.Int) ([]types.Header, error)
	TxByHash(common.Hash) (*types.Transaction, error)
	TxReceiptByHash(hash common.Hash) (*types.Receipt, error)
	StorageHash(common.Address, *big.Int) (common.Hash, error)
	FilterLogs(ethereum.FilterQuery) ([]types.Log, error)
}

type clnt struct {
	rpc RPC
}

func DialEthClient(rpcUrl string) (EthClient, error) {
	ctxwt, cancel := context.WithTimeout(context.Background(), defaultDialTimeout)
	defer cancel()

	bOff := retry.Exponential()
	rpcClient, err := retry.Do(ctxwt, defaultDialAttempts, bOff, func() (*rpc.Client, error) {
		if !common2.IsURLAvailable(rpcUrl) {
			return nil, fmt.Errorf("address unavailable (%s)", rpcUrl)
		}

		client, err := rpc.DialContext(ctxwt, rpcUrl)
		if err != nil {
			return nil, fmt.Errorf("failed to dial address (%s): %w", rpcUrl, err)
		}

		return client, nil
	})

	if err != nil {
		return nil, err
	}

	return &clnt{rpc: NewRPC(rpcClient)}, nil
}

func (c *clnt) BlockHeaderByHash(hash common.Hash) (*types.Header, error) {
	ctxwt, cancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
	defer cancel()

	var header *types.Header
	err := c.rpc.CallContext(ctxwt, &header, "eth_getBlockByHash", hash, false)
	if err != nil {
		return nil, err
	} else if header == nil {
		return nil, ethereum.NotFound
	}

	if header.Hash() != hash {
		return nil, errors.New("header mismatch")
	}

	return header, nil
}

func (c *clnt) BlockHeaderByNumber(number *big.Int) (*types.Header, error) {
	ctxwt, cancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
	defer cancel()

	var header *types.Header
	err := c.rpc.CallContext(ctxwt, &header, "eth_getBlockByNumber", toBlockNumArg(number), false)
	if err != nil {
		return nil, err
	} else if header == nil {
		return nil, ethereum.NotFound
	}

	return header, nil
}

func (c *clnt) BlockHeadersByRange(startHeight, endHeight *big.Int) ([]types.Header, error) {
	if startHeight.Cmp(endHeight) == 0 {
		header, err := c.BlockHeaderByNumber(startHeight)
		if err != nil {
			return nil, err
		}
		return []types.Header{*header}, nil
	}

	count := new(big.Int).Sub(endHeight, startHeight).Uint64() + 1
	batchElems := make([]rpc.BatchElem, count)
	for i := uint64(0); i < count; i++ {
		height := new(big.Int).Add(startHeight, new(big.Int).SetUint64(i))
		batchElems[i] = rpc.BatchElem{
			Method: "eth_getBlockByNumber",
			Args:   []interface{}{toBlockNumArg(height), false},
			Result: new(types.Header),
			Error:  nil,
		}
	}

	ctxwt, cancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
	defer cancel()
	err := c.rpc.BatchCallContext(ctxwt, batchElems)
	if err != nil {
		return nil, err
	}

	size := 0
	headers := make([]types.Header, count)
	for i, batchElem := range batchElems {
		if batchElem.Error != nil {
			return nil, batchElem.Error
		} else if batchElem.Result == nil {
			break
		}

		header, ok := batchElem.Result.(*types.Header)
		if !ok {
			return nil, fmt.Errorf("unable to transform rpc response %v into types.Header", batchElem.Result)
		}
		if i > 0 && header.ParentHash != headers[i-1].Hash() {
			return nil, fmt.Errorf("queried header %s does not follow parent %s", header.Hash(), headers[i-1].Hash())
		}

		headers[i] = *header
		size = size + 1
	}

	headers = headers[:size]
	return headers, nil
}

func (c *clnt) TxByHash(hash common.Hash) (*types.Transaction, error) {
	ctxwt, cancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
	defer cancel()

	var tx *types.Transaction
	err := c.rpc.CallContext(ctxwt, &tx, "eth_getTransactionReceipt", hash)
	if err != nil {
		return nil, err
	} else if tx == nil {
		return nil, ethereum.NotFound
	}

	return tx, nil
}

func (c *clnt) TxReceiptByHash(hash common.Hash) (*types.Receipt, error) {
	ctxwt, cancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
	defer cancel()

	var txReceipt *types.Receipt
	err := c.rpc.CallContext(ctxwt, &txReceipt, "eth_getTransactionByHash", hash)
	if err != nil {
		return nil, err
	} else if txReceipt == nil {
		return nil, ethereum.NotFound
	}

	return txReceipt, nil
}

func (c *clnt) StorageHash(address common.Address, blockNumber *big.Int) (common.Hash, error) {
	ctxwt, cancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
	defer cancel()

	proof := struct{ StorageHash common.Hash }{}
	err := c.rpc.CallContext(ctxwt, &proof, "eth_getProof", address, nil, toBlockNumArg(blockNumber))
	if err != nil {
		return common.Hash{}, err
	}

	return proof.StorageHash, nil
}

func (c *clnt) FilterLogs(query ethereum.FilterQuery) ([]types.Log, error) {
	ctxwt, cancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
	defer cancel()

	var result []types.Log
	arg, err := toFilterArg(query)
	if err != nil {
		return nil, err
	}

	err = c.rpc.CallContext(ctxwt, &result, "eth_getLogs", arg)
	return result, err
}

type RPC interface {
	Close()
	CallContext(ctx context.Context, result any, method string, args ...any) error
	BatchCallContext(ctx context.Context, b []rpc.BatchElem) error
}

type rpcClient struct {
	rpc *rpc.Client
}

func NewRPC(client *rpc.Client) RPC {
	return &rpcClient{client}
}

func (c *rpcClient) Close() {
	c.rpc.Close()
}

func (c *rpcClient) CallContext(ctx context.Context, result any, method string, args ...any) error {
	err := c.rpc.CallContext(ctx, result, method, args...)
	return err
}

func (c *rpcClient) BatchCallContext(ctx context.Context, b []rpc.BatchElem) error {
	err := c.rpc.BatchCallContext(ctx, b)
	return err
}

func toBlockNumArg(number *big.Int) string {
	if number == nil {
		return "latest"
	}
	if number.Sign() >= 0 {
		return hexutil.EncodeBig(number)
	}
	return string(rpc.BlockNumber(number.Int64()))
}

func toFilterArg(q ethereum.FilterQuery) (interface{}, error) {
	arg := map[string]interface{}{
		"address": q.Addresses,
		"topics":  q.Topics,
	}
	if q.BlockHash != nil {
		arg["blockHash"] = *q.BlockHash
		if q.FromBlock != nil || q.ToBlock != nil {
			return nil, errors.New("cannot specify both BlockHash and FromBlock/ToBlock")
		}
	} else {
		if q.FromBlock == nil {
			arg["fromBlock"] = "0x0"
		} else {
			arg["fromBlock"] = toBlockNumArg(q.FromBlock)
		}
		arg["toBlock"] = toBlockNumArg(q.ToBlock)
	}
	return arg, nil
}
