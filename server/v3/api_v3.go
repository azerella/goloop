package v3

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
	"unsafe"

	"github.com/icon-project/goloop/block"
	"github.com/icon-project/goloop/btp/ntm"
	"github.com/icon-project/goloop/common"
	"github.com/icon-project/goloop/common/db"
	"github.com/icon-project/goloop/common/errors"
	"github.com/icon-project/goloop/common/intconv"
	"github.com/icon-project/goloop/module"
	"github.com/icon-project/goloop/server/jsonrpc"
	"github.com/icon-project/goloop/server/metric"
	"github.com/icon-project/goloop/service"
	"github.com/icon-project/goloop/service/scoreresult"
	"github.com/icon-project/goloop/service/trace"
	"github.com/icon-project/goloop/service/txresult"
)

const (
	ConfigShowPatchTransaction = false
)

func MethodRepository(mtr *metric.JsonrpcMetric) *jsonrpc.MethodRepository {
	mr := jsonrpc.NewMethodRepository(mtr)
	RegisterValidationRule(mr.Validator())

	mr.RegisterMethod("icx_getLastBlock", getLastBlock)
	mr.RegisterMethod("icx_getBlockByHeight", getBlockByHeight)
	mr.RegisterMethod("icx_getBlockByHash", getBlockByHash)
	mr.RegisterMethod("icx_call", call)
	mr.RegisterMethod("icx_getBalance", getBalance)
	mr.RegisterMethod("icx_getScoreApi", getScoreApi)
	mr.RegisterMethod("icx_getTotalSupply", getTotalSupply)
	mr.RegisterMethod("icx_getTransactionResult", getTransactionResult)
	mr.RegisterMethod("icx_getTransactionByHash", getTransactionByHash)
	mr.RegisterMethod("icx_sendTransaction", sendTransaction)
	mr.RegisterMethod("icx_sendTransactionAndWait", sendTransactionAndWait)
	mr.RegisterMethod("icx_waitTransactionResult", waitTransactionResult)

	mr.RegisterMethod("icx_getDataByHash", getDataByHash)
	mr.RegisterMethod("icx_getBlockHeaderByHeight", getBlockHeaderByHeight)
	mr.RegisterMethod("icx_getVotesByHeight", getVotesByHeight)
	mr.RegisterMethod("icx_getProofForResult", getProofForResult)
	mr.RegisterMethod("icx_getProofForEvents", getProofForEvents)
	mr.RegisterMethod("icx_getScoreStatus", getScoreStatus)

	mr.RegisterMethod("btp_getNetworkInfo", getBTPNetworkInfo)
	mr.RegisterMethod("btp_getNetworkTypeInfo", getBTPNetworkTypeInfo)
	mr.RegisterMethod("btp_getMessages", getBTPMessages)
	mr.RegisterMethod("btp_getHeader", getBTPHeader)
	mr.RegisterMethod("btp_getProof", getBTPProof)
	mr.RegisterMethod("btp_getSourceInformation", getBTPSourceInformation)

	mr.SetAllowedNotification("icx_sendTransaction")
	mr.SetAllowedNotification("icx_sendTransactionAndWait")
	return mr
}

func fillTransactions(blockJson interface{}, b module.Block, v module.JSONVersion) error {
	result := blockJson.(map[string]interface{})

	if ConfigShowPatchTransaction {
		if txs, err := convertTransactionList(b.PatchTransactions(), v); err != nil {
			return err
		} else {
			if len(txs) > 0 {
				result["patch_transaction_list"] = txs
			}
		}
	}

	if txs, err := convertTransactionList(b.NormalTransactions(), v); err != nil {
		return err
	} else {
		result["confirmed_transaction_list"] = txs
	}
	return nil
}

func checkBaseHeight(c module.Chain, height int64) error {
	if height < 0 {
		return errors.NotFoundError.Errorf("NegativeHeight(height=%d)", height)
	}
	base := c.GenesisStorage().Height()
	if height < base {
		return errors.NotFoundError.Errorf(
			"PrunedBlock(height=%d,base=%d)", height, base)
	}
	return nil
}

func getLastBlock(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()
	var param struct{}
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	if bm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	block, err := bm.GetLastBlock()
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	blockJson, err := block.ToJSON(module.JSONVersion3)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	if err := fillTransactions(blockJson, block, module.JSONVersion3); err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	return blockJson, nil
}

func getBlockByHeight(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param BlockHeightParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}
	height, err := param.Height.ParseInt(64)
	if err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	if err := checkBaseHeight(chain, height); err != nil {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	if bm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	block, err := bm.GetBlockByHeight(height)
	if errors.NotFoundError.Equals(err) {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	blockJson, err := block.ToJSON(module.JSONVersion3)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	if err := fillTransactions(blockJson, block, module.JSONVersion3); err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	return blockJson, nil
}

func getBlockByHash(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param BlockHashParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	if bm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	block, err := bm.GetBlock(param.Hash.Bytes())
	if errors.NotFoundError.Equals(err) {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	if err := checkBaseHeight(chain, block.Height()); err != nil {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	}

	blockJson, err := block.ToJSON(module.JSONVersion3)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	if err := fillTransactions(blockJson, block, module.JSONVersion3); err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	return blockJson, nil
}

func call(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param CallParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	sm := chain.ServiceManager()
	if bm == nil || sm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	block, err := getBlock(chain, bm, param.Height)
	if err != nil {
		if errors.NotFoundError.Equals(err) {
			return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
		}
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	bi := common.NewBlockInfo(block.Height(), block.Timestamp())
	result, err := sm.Call(block.Result(), block.NextValidators(), params.RawMessage(), bi)
	if err != nil {
		if service.InvalidQueryError.Equals(err) {
			return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
		} else if scoreresult.IsValid(err) {
			return nil, jsonrpc.ErrScore(err, debug)
		} else {
			return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
		}
	} else {
		return result, nil
	}
}

func getBlock(chain module.Chain, bm module.BlockManager, height jsonrpc.HexInt) (block module.Block, err error) {
	if height == "" {
		block, err = bm.GetLastBlock()
	} else {
		h, _ := height.Int64()
		if err := checkBaseHeight(chain, h); err != nil {
			return nil, err
		}
		block, err = bm.GetBlockByHeight(h)
	}
	return
}

func getBalance(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	var param AddressParam
	debug := ctx.IncludeDebug()
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	sm := chain.ServiceManager()
	if bm == nil || sm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	var balance common.HexInt
	block, err := getBlock(chain, bm, param.Height)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	b, err := sm.GetBalance(block.Result(), param.Address.Address())
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	balance.Set(b)
	return &balance, nil
}

func getScoreApi(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	var param ScoreAddressParam
	debug := ctx.IncludeDebug()
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}
	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}
	bm := chain.BlockManager()
	sm := chain.ServiceManager()
	if bm == nil || sm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}
	b, err := getBlock(chain, bm, param.Height)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	info, err := sm.GetAPIInfo(b.Result(), param.Address.Address())
	if service.NoActiveContractError.Equals(err) {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	}
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	if jso, err := info.ToJSON(module.JSONVersion3); err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	} else {
		return jso, nil
	}
}

func getTotalSupply(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()
	var param *HeightParam
	var height jsonrpc.HexInt
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	} else {
		if param != nil {
			height = param.Height
		}
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}
	bm := chain.BlockManager()
	sm := chain.ServiceManager()
	if bm == nil || sm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	b, err := getBlock(chain, bm, height)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	var tsValue common.HexInt
	ts, err := sm.GetTotalSupply(b.Result())
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	tsValue.Set(ts)

	return &tsValue, nil
}

func getTransactionResult(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param TransactionHashParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	sm := chain.ServiceManager()
	if bm == nil || sm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	txInfo, err := bm.GetTransactionInfo(param.Hash.Bytes())
	if errors.NotFoundError.Equals(err) {
		if sm.HasTransaction(param.Hash.Bytes()) {
			return nil, jsonrpc.ErrorCodePending.New("Pending")
		}
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	blk := txInfo.Block()
	if err := checkBaseHeight(chain, blk.Height()); err != nil {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	}
	receipt, err := txInfo.GetReceipt()
	if block.ResultNotFinalizedError.Equals(err) {
		return nil, jsonrpc.ErrorCodeExecuting.New("Executing")
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	res, err := receipt.ToJSON(module.JSONVersion3)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	result := res.(map[string]interface{})
	result["blockHash"] = "0x" + hex.EncodeToString(blk.ID())
	result["blockHeight"] = "0x" + strconv.FormatInt(int64(blk.Height()), 16)
	result["txIndex"] = "0x" + strconv.FormatInt(int64(txInfo.Index()), 16)
	result["txHash"] = "0x" + hex.EncodeToString(param.Hash.Bytes())

	return result, nil
}

func getTransactionByHash(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param TransactionHashParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	if bm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	txInfo, err := bm.GetTransactionInfo(param.Hash.Bytes())
	if errors.NotFoundError.Equals(err) {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	tx, err := txInfo.Transaction()
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	res, err := tx.ToJSON(module.JSONVersion3)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	blk := txInfo.Block()
	result := res.(map[string]interface{})
	result["blockHash"] = "0x" + hex.EncodeToString(blk.ID())
	result["blockHeight"] = "0x" + strconv.FormatInt(int64(blk.Height()), 16)
	result["txIndex"] = "0x" + strconv.FormatInt(int64(txInfo.Index()), 16)

	return result, nil
}

func sendTransaction(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param TransactionParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	sm := chain.ServiceManager()

	var state []byte
	var height int64
	if chain.ValidateTxOnSend() {
		bm := chain.BlockManager()
		if bm == nil {
			return nil, jsonrpc.ErrorCodeServer.New("Stopped")
		}
		block, err := bm.GetLastBlock()
		if err != nil {
			return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
		}
		state = block.Result()
		height = block.Height() + 1
	}

	hash, err := sm.SendTransaction(state, height, params.RawMessage())
	if err != nil {
		if service.TransactionPoolOverflowError.Equals(err) {
			return nil, jsonrpc.ErrorCodeTxPoolOverflow.Wrap(err, debug)
		}
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	result := "0x" + hex.EncodeToString(hash)

	return result, nil
}

func getDataByHash(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param DataHashParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	dbm := chain.Database()

	bucket, err := dbm.GetBucket(db.BytesByHash)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	value, err := bucket.Get(param.Hash.Bytes())
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	if value == nil {
		return nil, jsonrpc.ErrorCodeNotFound.New("Fail to find data")
	}

	return value, nil
}

func getBlockHeaderByHeight(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param BlockHeightParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}
	height, err := param.Height.ParseInt(64)
	if err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	if err := checkBaseHeight(chain, height); err != nil {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	if bm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	block, err := bm.GetBlockByHeight(height)
	if errors.NotFoundError.Equals(err) {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	buf := bytes.NewBuffer(nil)
	if err := block.MarshalHeader(buf); err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	return buf.Bytes(), nil
}

func getVotesByHeight(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param BlockHeightParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}
	height, err := param.Height.ParseInt(64)
	if err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	if err := checkBaseHeight(chain, height); err != nil {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	}

	cs := chain.Consensus()

	votes, err := cs.GetVotesByHeight(height)
	if errors.NotFoundError.Equals(err) {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	return votes.Bytes(), nil
}

func getProofForResult(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param ProofResultParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}
	var idx int
	if v64, err := param.Index.ParseInt(int(unsafe.Sizeof(idx)) * 8); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	} else {
		idx = int(v64)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	sm := chain.ServiceManager()
	if bm == nil || sm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	block, err := bm.GetBlock(param.BlockHash.Bytes())
	if errors.NotFoundError.Equals(err) {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	if err := checkBaseHeight(chain, block.Height()); err != nil {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	}

	blockResult := block.Result()
	receiptList, err := sm.ReceiptListFromResult(blockResult, module.TransactionGroupNormal)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	proofs, err := receiptList.GetProof(idx)
	if err != nil {
		if errors.NotFoundError.Equals(err) {
			return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
		}
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	return proofs, nil
}

func getProofForEvents(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param ProofEventsParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}
	var idx int
	if v64, err := param.Index.ParseInt(int(unsafe.Sizeof(idx)) * 8); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	} else {
		idx = int(v64)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	sm := chain.ServiceManager()
	if bm == nil || sm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	block, err := bm.GetBlock(param.BlockHash.Bytes())
	if errors.NotFoundError.Equals(err) {
		err = errors.NotFoundError.Wrapf(err,
			"fail to get a block for hash=%#x", param.BlockHash.Bytes())
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	if err := checkBaseHeight(chain, block.Height()); err != nil {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	}

	blockResult := block.Result()
	receiptList, err := sm.ReceiptListFromResult(blockResult, module.TransactionGroupNormal)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	receipt, err := receiptList.Get(idx)
	if err != nil {
		err = errors.NotFoundError.Wrapf(err,
			"fail to get a receipt for index=%d", idx)
		if errors.NotFoundError.Equals(err) {
			return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
		}
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	proofs := [][][]byte{}
	rProof, err := receiptList.GetProof(idx)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	proofs = append(proofs, rProof)
	for _, idx := range param.Events {
		proof, err := receipt.GetProofOfEvent(int(idx.Value()))
		if errors.InvalidStateError.Equals(err) {
			break
		}
		if errors.NotFoundError.Equals(err) {
			err = errors.NotFoundError.Wrapf(err,
				"fail to get a proof for event index=%d", idx.Value())
			return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
		}
		proofs = append(proofs, proof)
	}
	return proofs, nil
}

func getScoreStatus(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	var param ScoreAddressParam
	debug := ctx.IncludeDebug()
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}
	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}
	bm := chain.BlockManager()
	sm := chain.ServiceManager()
	if bm == nil || sm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}
	b, err := getBlock(chain, bm, param.Height)
	if err != nil {
		if errors.NotFoundError.Equals(err) {
			return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
		}
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	s, err := sm.GetSCOREStatus(b.Result(), param.Address.Address())
	if err != nil {
		if errors.NotFoundError.Equals(err) {
			return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
		}
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	jso, err := s.ToJSON(b.Height(), module.JSONVersion3)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	return jso, nil
}

func getBTPNetworkInfo(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param BTPQueryParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	height, err := param.Height.Int64()
	if err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}
	nid, err := param.Id.Int64()
	if err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	sm := chain.ServiceManager()
	if bm == nil || sm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}
	block, err := getBlock(chain, bm, param.Height)
	if errors.NotFoundError.Equals(err) {
		err = errors.NotFoundError.Wrapf(err,
			"fail to get a block for height=%d", height)
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	blockResult := block.Result()
	nw, err := sm.BTPNetworkFromResult(blockResult, nid)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	nt, err := sm.BTPNetworkTypeFromResult(blockResult, nw.NetworkTypeID())
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	res := nw.ToJSON()
	res["networkID"] = intconv.FormatInt(nid)
	res["networkTypeName"] = nt.UID()
	return res, nil
}

func getBTPNetworkTypeInfo(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param BTPQueryParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	height, err := param.Height.Int64()
	if err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}
	ntid, err := param.Id.Int64()
	if err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	sm := chain.ServiceManager()
	if bm == nil || sm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}
	block, err := getBlock(chain, bm, param.Height)
	if errors.NotFoundError.Equals(err) {
		err = errors.NotFoundError.Wrapf(err, "fail to get a block for height=%d", height)
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	blockResult := block.Result()
	nt, err := sm.BTPNetworkTypeFromResult(blockResult, ntid)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	res := nt.ToJSON()
	res["networkTypeID"] = intconv.FormatInt(ntid)
	return res, nil
}

func getBTPMessages(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param BTPMessagesParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	height, err := param.Height.Int64()
	if err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}
	nid, err := param.NetworkId.Int64()
	if err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	sm := chain.ServiceManager()
	if bm == nil || sm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	if err := checkBaseHeight(chain, height); err != nil {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	}
	block, err := bm.GetBlockByHeight(height)
	if errors.NotFoundError.Equals(err) {
		err = errors.NotFoundError.Wrapf(err, "fail to get a block for height=%d", height)
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	res := make([]string, 0)
	blockResult := block.Result()
	bDigest, err := sm.BTPDigestFromResult(blockResult)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	if bDigest == nil {
		return res, nil
	}
	nw, err := sm.BTPNetworkFromResult(blockResult, nid)
	if err != nil || nw == nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	ntid := nw.NetworkTypeID()
	nt, err := sm.BTPNetworkTypeFromResult(blockResult, ntid)
	if err != nil || nt == nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	ntDigest := bDigest.NetworkTypeDigestFor(ntid)
	if ntDigest == nil {
		return res, nil
	}
	nwDigest := ntDigest.NetworkDigestFor(nid)
	if nwDigest == nil {
		return res, nil
	}
	ml, err := nwDigest.MessageList(chain.Database(), ntm.ForUID(nt.UID()))
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	size := int(ml.Len())
	for i := 0; i < size; i++ {
		msg, err := ml.Get(i)
		if err != nil {
			return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
		}
		res = append(res, base64.StdEncoding.EncodeToString(msg.Bytes()))
	}
	return res, nil
}

func getBTPHeader(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param BTPMessagesParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	height, err := param.Height.Int64()
	if err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}
	nid, err := param.NetworkId.Int64()
	if err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}
	bm := chain.BlockManager()
	cs := chain.Consensus()
	if bm == nil || cs == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	if err := checkBaseHeight(chain, height); err != nil {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	}
	block, err := bm.GetBlockByHeight(height)
	if errors.NotFoundError.Equals(err) {
		err = errors.NotFoundError.Wrapf(err, "fail to get a block for height=%d", height)
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	btpBlock, _, err := cs.GetBTPBlockHeaderAndProof(block, nid, module.FlagBTPBlockHeader)
	if errors.NotFoundError.Equals(err) {
		err = errors.NotFoundError.Wrapf(err, "fail to get a BTP block header for height=%d, nid=%d", height, nid)
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	return base64.StdEncoding.EncodeToString(btpBlock.HeaderBytes()), nil
}

func getBTPProof(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param BTPMessagesParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	height, err := param.Height.Int64()
	if err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}
	nid, err := param.NetworkId.Int64()
	if err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}
	bm := chain.BlockManager()
	cs := chain.Consensus()
	if bm == nil || cs == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	if err := checkBaseHeight(chain, height); err != nil {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	}
	block, err := bm.GetBlockByHeight(height)
	if errors.NotFoundError.Equals(err) {
		err = errors.NotFoundError.Wrapf(err, "fail to get a block for height=%d", height)
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	_, proof, err := cs.GetBTPBlockHeaderAndProof(block, nid, module.FlagBTPBlockProof)
	if errors.NotFoundError.Equals(err) {
		err = errors.NotFoundError.Wrapf(err, "fail to get a BTP block proof for height=%d, nid=%d", height, nid)
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	return base64.StdEncoding.EncodeToString(proof), nil
}

func getBTPSourceInformation(ctx *jsonrpc.Context, _ *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()
	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}
	bm := chain.BlockManager()
	sm := chain.ServiceManager()
	if bm == nil || sm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	block, err := bm.GetLastBlock()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}
	ntids, err := sm.BTPNetworkTypeIDsFromResult(block.Result())
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	res := make(map[string]interface{})
	res["srcNetworkUID"] = intconv.FormatInt(int64(chain.NID())) + ".icon"
	ontids := make([]interface{}, len(ntids))
	for i, ntid := range ntids {
		ontids[i] = intconv.FormatInt(ntid)
	}
	res["networkTypeIDs"] = ontids
	return res, nil
}

// convert TransactionList to []Transaction
func convertTransactionList(txs module.TransactionList, version module.JSONVersion) ([]interface{}, error) {
	list := []interface{}{}

	for it := txs.Iterator(); it.Has(); it.Next() {
		tx, _, err := it.Get()
		if err != nil {
			return nil, err
		}

		res, err := tx.ToJSON(version)
		if err != nil {
			return nil, err
		}
		list = append(list, res)
	}
	return list, nil
}

func sendTransactionAndWait(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	dt := chain.DefaultWaitTimeout()
	if dt <= 0 {
		return nil, jsonrpc.ErrorCodeMethodNotFound.New("NotEnabled")
	}

	ut := ctx.GetTimeout(dt)
	if ut <= 0 {
		return nil, jsonrpc.ErrorCodeInvalidRequest.Errorf("InvalidTimeout(%dms)", ut/time.Millisecond)
	}
	mt := chain.MaxWaitTimeout()
	timeout := ut
	maxLimit := false
	if timeout > mt {
		timeout = mt
		maxLimit = true
	}

	var param TransactionParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	if bm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	var state []byte
	var height int64
	if chain.ValidateTxOnSend() {
		block, err := bm.GetLastBlock()
		if err != nil {
			return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
		}
		state = block.Result()
		height = block.Height() + 1
	}

	hash, fc, err := bm.SendTransactionAndWait(state, height, params.RawMessage())
	if err != nil {
		if service.TransactionPoolOverflowError.Equals(err) {
			return nil, jsonrpc.ErrorCodeTxPoolOverflow.Wrap(err, debug)
		}
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	return waitTransactionResultOnChannel(ctx, chain, bm, hash, debug, timeout, maxLimit, fc)
}

func waitTransactionResult(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	dt := chain.DefaultWaitTimeout()
	if dt <= 0 {
		return nil, jsonrpc.ErrorCodeMethodNotFound.Errorf("NotEnabled(waitTimeout=%d)", dt)
	}

	ut := ctx.GetTimeout(dt)
	if ut <= 0 {
		return nil, jsonrpc.ErrorCodeInvalidParams.Errorf("InvalidTimeout(%d)", ut)
	}
	mt := chain.MaxWaitTimeout()
	timeout := ut
	maxLimit := false
	if timeout > mt {
		timeout = mt
		maxLimit = true
	}

	var param TransactionHashParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	if bm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	hash := param.Hash.Bytes()
	fc, err := bm.WaitTransactionResult(hash)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	return waitTransactionResultOnChannel(ctx, chain, bm, hash, debug, timeout, maxLimit, fc)
}

func waitTransactionResultOnChannel(ctx *jsonrpc.Context, chain module.Chain, bm module.BlockManager, id []byte, debug bool, timeout time.Duration, maxLimit bool, fc <-chan interface{}) (interface{}, error) {
	tc := time.After(timeout)

	var err error
	var txInfo module.TransactionInfo
	var receipt module.Receipt
	select {
	case result := <-fc:
		switch ro := result.(type) {
		case error:
			return nil, jsonrpc.ErrorCodeSystem.Wrap(ro, debug)
		case module.TransactionInfo:
			txInfo = ro
			receipt, err = txInfo.GetReceipt()
			if err != nil {
				return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
			}
		case module.Receipt:
			txInfo, err = bm.GetTransactionInfo(id)
			if err != nil {
				return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
			}
			receipt = ro
		default:
			return nil, jsonrpc.ErrorCodeSystem.New("Unknown resulting object")
		}
	case <-tc:
		if maxLimit {
			return nil, jsonrpc.ErrorCodeSystemTimeout.New(
				fmt.Sprintf("SystemTimeoutExpire(dur=%s)", timeout),
				"0x"+hex.EncodeToString(id),
			)
		}
		return nil, jsonrpc.ErrorCodeTimeout.New(
			fmt.Sprintf("UserTimeoutExpire(dur=%s)", timeout),
			"0x"+hex.EncodeToString(id),
		)
	case <-ctx.Request().Context().Done():
		return nil, nil
	}

	blk := txInfo.Block()
	if err := checkBaseHeight(chain, blk.Height()); err != nil {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	}
	res, err := receipt.ToJSON(module.JSONVersion3)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	result := res.(map[string]interface{})
	result["blockHash"] = "0x" + hex.EncodeToString(blk.ID())
	result["blockHeight"] = "0x" + strconv.FormatInt(int64(blk.Height()), 16)
	result["txIndex"] = "0x" + strconv.FormatInt(int64(txInfo.Index()), 16)
	result["txHash"] = "0x" + hex.EncodeToString(id)

	return result, nil
}

func DebugMethodRepository(mtr *metric.JsonrpcMetric) *jsonrpc.MethodRepository {
	mr := jsonrpc.NewMethodRepository(mtr)
	RegisterValidationRule(mr.Validator())

	mr.RegisterMethod("debug_getTrace", getTrace)
	mr.RegisterMethod("debug_estimateStep", estimateStep)

	return mr
}

func getTrace(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param TransactionHashParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	sm := chain.ServiceManager()
	if bm == nil || sm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	txInfo, err := bm.GetTransactionInfo(param.Hash.Bytes())
	if errors.NotFoundError.Equals(err) {
		if sm.HasTransaction(param.Hash.Bytes()) {
			return nil, jsonrpc.ErrorCodePending.New("Pending")
		}
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	if txInfo.Group() == module.TransactionGroupPatch {
		return nil, jsonrpc.ErrorCodeInvalidParams.New("Patch transaction can't be replayed")
	}

	blk := txInfo.Block()
	if err = checkBaseHeight(chain, blk.Height()); err != nil {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	}
	_, err = txInfo.GetReceipt()
	if block.ResultNotFinalizedError.Equals(err) {
		return nil, jsonrpc.ErrorCodeExecuting.New("Executing")
	} else if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	csi, err := bm.NewConsensusInfo(blk)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	nblk, err := bm.GetBlockByHeight(blk.Height() + 1)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	tr1, err := sm.CreateInitialTransition(blk.Result(), blk.NextValidators())
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	tr2, err := sm.CreateTransition(tr1, blk.NormalTransactions(), blk, csi, true)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	tr2 = sm.PatchTransition(tr2, nblk.PatchTransactions(), nblk)

	cb := &traceCallback{
		logs:    make([]interface{}, 0, 100),
		channel: make(chan interface{}, 10),
	}
	ti := module.TraceInfo{
		TraceMode: module.TraceModeInvoke,
		Range:     module.TraceRangeTransaction,
		Group:     txInfo.Group(),
		Index:     txInfo.Index(),
		Callback:  cb,
	}
	canceller, err := tr2.ExecuteForTrace(ti)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	timer := time.After(time.Second * 5)
	for {
		select {
		case <-timer:
			canceller()
			return nil, jsonrpc.ErrorCodeSystemTimeout.Errorf(
				"Not enough time to get result of %x", param.Hash.Bytes())
		case <-cb.channel:
			return cb.invokeTraceToJSON(), nil
		}
	}
	return nil, jsonrpc.ErrorCodeSystem.New("Unknown error on channel")
}

func estimateStep(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	var param TransactionParamForEstimate
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	sm := chain.ServiceManager()
	if bm == nil || sm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("ChannelStopped")
	}

	// get last block
	blk, err := bm.GetLastBlock()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	// new block information based on the last
	oldTS := blk.Timestamp()
	newTS := common.UnixMicroFromTime(time.Now())
	if newTS <= oldTS {
		newTS = oldTS + 1
	}
	bi := common.NewBlockInfo(blk.Height()+1, newTS)

	// execute transaction
	rct, err := sm.ExecuteTransaction(
		blk.Result(),
		blk.NextValidators().Hash(),
		params.RawMessage(),
		bi,
	)
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}
	if status := rct.Status(); status != module.StatusSuccess {
		if rctex, ok := rct.(txresult.Receipt); ok {
			if err := rctex.Reason(); err != nil {
				return nil, jsonrpc.ErrScore(rctex.Reason(), debug)
			}
		}
		return nil, jsonrpc.ErrScoreWithStatus(status)
	}
	steps := new(common.HexInt)
	steps.Set(rct.StepUsed())
	return steps, nil
}

const CIDForMainNet = 0x1

func getTraceForRosetta(ctx *jsonrpc.Context, params *jsonrpc.Params) (interface{}, error) {
	debug := ctx.IncludeDebug()

	var param RosettaTraceParam
	if err := params.Convert(&param); err != nil {
		return nil, jsonrpc.ErrorCodeInvalidParams.Wrap(err, debug)
	}

	chain, err := ctx.Chain()
	if err != nil {
		return nil, jsonrpc.ErrorCodeServer.Wrap(err, debug)
	}

	bm := chain.BlockManager()
	sm := chain.ServiceManager()
	if bm == nil || sm == nil {
		return nil, jsonrpc.ErrorCodeServer.New("Stopped")
	}

	blk, txInfo, err := findBlockAndTxInfoByRosettaTraceParam(chain.CID(), bm, sm, param, debug)
	if err != nil {
		return nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
	}

	csi, err := bm.NewConsensusInfo(blk)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	nblk, err := bm.GetBlockByHeight(blk.Height() + 1)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	tr1, err := sm.CreateInitialTransition(blk.Result(), blk.NextValidators())
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	tr2, err := sm.CreateTransition(tr1, blk.NormalTransactions(), blk, csi, true)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}
	tr2 = sm.PatchTransition(tr2, nblk.PatchTransactions(), nblk)

	rl, err := sm.ReceiptListFromResult(nblk.Result(), module.TransactionGroupNormal)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	var replacer trace.TxHashReplacer
	if chain.CID() == CIDForMainNet {
		replacer = trace.ReplaceMissingTxHash
	}
	cb := &traceCallback{
		channel: make(chan interface{}, 10),
		bt:      trace.NewBalanceTracer(10, replacer),
	}
	ti := module.TraceInfo{
		TraceMode:  module.TraceModeBalanceChange,
		TraceBlock: trace.NewTraceBlock(blk.ID(), rl),
		Callback:   cb,
	}
	if txInfo != nil {
		ti.Range = module.TraceRangeTransaction
		ti.Group = module.TransactionGroupNormal
		ti.Index = txInfo.Index()
	} else {
		if len(param.Tx) > 0 {
			ti.Range = module.TraceRangeBlockTransaction
		} else {
			ti.Range = module.TraceRangeBlock
		}
	}
	canceller, err := tr2.ExecuteForTrace(ti)
	if err != nil {
		return nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
	}

	timer := time.After(time.Second * 60)
	for {
		select {
		case <-timer:
			canceller()
			return nil, jsonrpc.ErrorCodeSystemTimeout.Errorf(
				"Not enough time to get result of %+v", param)
		case <-cb.channel:
			return cb.balanceChangeToJSON(blk), nil
		}
	}
	return nil, jsonrpc.ErrorCodeSystem.New("Unknown error on channel")
}

func findBlockAndTxInfoByRosettaTraceParam(
	cid int,
	bm module.BlockManager,
	sm module.ServiceManager,
	param RosettaTraceParam,
	debug bool,
) (module.Block, module.TransactionInfo, error) {
	var blk module.Block
	var txInfo module.TransactionInfo
	var err error

	if len(param.Tx) > 0 {
		if param.Tx[0:2] == "bx" {
			if blk, err = bm.GetBlock(param.Tx.Bytes()); err != nil {
				return nil, nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
			}
		} else {
			txInfo, err = getTransactionInfo(param.Tx.Bytes(), cid, bm, sm)
			if errors.NotFoundError.Equals(err) {
				if sm.HasTransaction(param.Tx.Bytes()) {
					return nil, nil, jsonrpc.ErrorCodePending.New("Pending")
				}
				return nil, nil, jsonrpc.ErrorCodeNotFound.Wrap(err, debug)
			} else if err != nil {
				return nil, nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
			}
			if txInfo.Group() == module.TransactionGroupPatch {
				return nil, nil, jsonrpc.ErrorCodeInvalidParams.New("Patch transaction can't be replayed")
			}
			_, err = txInfo.GetReceipt()
			if block.ResultNotFinalizedError.Equals(err) {
				return nil, nil, jsonrpc.ErrorCodeExecuting.New("Executing")
			} else if err != nil {
				return nil, nil, jsonrpc.ErrorCodeSystem.Wrap(err, debug)
			}
			blk = txInfo.Block()
		}
	} else if len(param.Block) > 0 {
		blk, err = bm.GetBlock(param.Block.Bytes())
	} else if len(param.Height) > 0 {
		blk, err = bm.GetBlockByHeight(param.Height.Value())
	} else {
		// Last block
		if blk, err = bm.GetLastBlock(); err == nil && blk.Height() > 0 {
			// Transactions in the last block are not finalized in onTheNext blockchain,
			// so the previous one of the last block is actually considered the last block in rosetta_getTrace()
			blk, err = bm.GetBlockByHeight(blk.Height() - 1)
		}
	}
	return blk, txInfo, err
}

type missingTransactionInfo struct {
	blk   module.Block
	index int
	sm    module.ServiceManager
	nblk  module.Block
}

func (m *missingTransactionInfo) Block() module.Block {
	return m.blk
}

func (m *missingTransactionInfo) Index() int {
	return m.index
}

func (m *missingTransactionInfo) Group() module.TransactionGroup {
	return module.TransactionGroupNormal
}

func (m *missingTransactionInfo) Transaction() (module.Transaction, error) {
	return nil, errors.UnsupportedError.Errorf("Not implemented")
}

func (m *missingTransactionInfo) GetReceipt() (module.Receipt, error) {
	if m.nblk != nil {
		rl, err := m.sm.ReceiptListFromResult(
			m.nblk.Result(), m.Group())
		if err != nil {
			return nil, err
		}
		rct, err := rl.Get(m.index)
		if err != nil {
			return nil, err
		}
		return rct, nil
	} else {
		return nil, block.ErrResultNotFinalized
	}
}

func getTransactionInfo(
	txHash []byte, cid int, bm module.BlockManager, sm module.ServiceManager) (module.TransactionInfo, error) {
	if cid == CIDForMainNet {
		height, index, ok := trace.GetMissingTxLocator(txHash)
		if ok {
			if blk, err := bm.GetBlockByHeight(height); err == nil {
				nblk, _ := bm.GetBlockByHeight(height + 1)
				return &missingTransactionInfo{blk, index, sm, nblk}, nil
			}
		}
	}
	return bm.GetTransactionInfo(txHash)
}

func RosettaMethodRepository(mtr *metric.JsonrpcMetric) *jsonrpc.MethodRepository {
	mr := jsonrpc.NewMethodRepository(mtr)

	mr.RegisterMethod("rosetta_getTrace", getTraceForRosetta)

	return mr
}
