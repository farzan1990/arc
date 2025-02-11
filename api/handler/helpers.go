package handler

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/bitcoin-sv/arc/api/transactionHandler"
	"github.com/opentracing/opentracing-go"
	"github.com/ordishs/go-bitcoin"
	"github.com/ordishs/gocore"
)

func getTransactionFromNode(ctx context.Context, inputTxID string) ([]byte, error) {
	span, _ := opentracing.StartSpanFromContext(ctx, "getTransactionFromNode")
	defer span.Finish()

	// get from our node, if configured
	peerURL, err, peerURLFound := gocore.Config().GetURL("peer_rpc")
	if err != nil {
		return nil, err
	}

	if peerURLFound {
		// get the transaction from the bitcoin node rpc
		port, _ := strconv.Atoi(peerURL.Port())
		password, _ := peerURL.User.Password()
		node, err := bitcoin.New(peerURL.Hostname(), port, peerURL.User.Username(), password, false)
		if err != nil {
			return nil, err
		}

		var tx *bitcoin.RawTransaction
		tx, err = node.GetRawTransaction(inputTxID)
		if err != nil {
			return nil, err
		}

		var txBytes []byte
		txBytes, err = hex.DecodeString(tx.Hex)
		if err != nil {
			return nil, err
		}

		if txBytes != nil {
			return txBytes, nil
		}
	}

	return nil, transactionHandler.ErrParentTransactionNotFound
}

func getTransactionFromWhatsOnChain(ctx context.Context, inputTxID string) ([]byte, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "getTransactionFromWhatsOnChain")
	defer span.Finish()

	wocApiKey, _ := gocore.Config().Get("wocApiKey")
	if wocApiKey != "" {
		wocURL := fmt.Sprintf("https://api.whatsonchain.com/v1/bsv/%s/tx/%s/hex", "main", inputTxID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, wocURL, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", wocApiKey)

		var resp *http.Response
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return nil, transactionHandler.ErrParentTransactionNotFound
		}

		var txHexBytes []byte
		txHexBytes, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		txHex := string(txHexBytes)

		var txBytes []byte
		txBytes, err = hex.DecodeString(txHex)
		if err != nil {
			return nil, err
		}

		if txBytes != nil {
			return txBytes, nil
		}
	}

	return nil, transactionHandler.ErrParentTransactionNotFound
}
