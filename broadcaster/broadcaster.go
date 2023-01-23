package broadcaster

import (
	"context"
	"encoding/hex"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/TAAL-GmbH/arc/lib/fees"
	"github.com/TAAL-GmbH/arc/lib/keyset"
	"github.com/TAAL-GmbH/arc/metamorph/metamorph_api"
	"github.com/labstack/gommon/random"
	"github.com/libsv/go-bk/base58"
	"github.com/libsv/go-bt/v2"
	"github.com/libsv/go-bt/v2/unlocker"
	"github.com/ordishs/go-bitcoin"
	"github.com/ordishs/gocore"
)

var stdFee = &bt.Fee{
	FeeType: bt.FeeTypeStandard,
	MiningFee: bt.FeeUnit{
		Satoshis: 50,
		Bytes:    1000,
	},
	RelayFee: bt.FeeUnit{
		Satoshis: 0,
		Bytes:    1000,
	},
}

var dataFee = &bt.Fee{
	FeeType: bt.FeeTypeData,
	MiningFee: bt.FeeUnit{
		Satoshis: 50,
		Bytes:    1000,
	},
	RelayFee: bt.FeeUnit{
		Satoshis: 0,
		Bytes:    1000,
	},
}

type ClientI interface {
	PutTransaction(ctx context.Context, tx *bt.Tx) (*metamorph_api.TransactionStatus, error)
	GetTransactionStatus(ctx context.Context, txID string) (*metamorph_api.TransactionStatus, error)
}

type Broadcaster struct {
	Client     ClientI
	FromKeySet *keyset.KeySet
	ToKeySet   *keyset.KeySet
	Outputs    int64
	IsDryRun   bool
	IsRegtest  bool
	FeeQuote   *bt.FeeQuote
	txs        []*bt.Tx
}

func New(client ClientI, fromKeySet *keyset.KeySet, toKeySet *keyset.KeySet, outputs int64) *Broadcaster {
	var fq = bt.NewFeeQuote()
	fq.AddQuote(bt.FeeTypeStandard, stdFee)
	fq.AddQuote(bt.FeeTypeData, dataFee)

	return &Broadcaster{
		Client:     client,
		FromKeySet: fromKeySet,
		ToKeySet:   toKeySet,
		Outputs:    outputs,
		IsRegtest:  true,
		FeeQuote:   fq,
		txs:        make([]*bt.Tx, outputs),
	}
}

func (b *Broadcaster) ConsolidateOutputsToOriginal(ctx context.Context) error {
	// consolidate all transactions back into the original address
	log.Println("Consolidating all transactions back into original address")
	consolidationAddress := b.FromKeySet.Address(!b.IsRegtest)
	consolidationTx := bt.NewTx()
	utxos := make([]*bt.UTXO, len(b.txs))
	totalSatoshis := uint64(0)
	for i, tx := range b.txs {
		utxos[i] = &bt.UTXO{
			TxID:          tx.TxIDBytes(),
			Vout:          0,
			LockingScript: tx.Outputs[0].LockingScript,
			Satoshis:      tx.Outputs[0].Satoshis,
		}
		totalSatoshis += tx.Outputs[0].Satoshis
	}
	err := consolidationTx.FromUTXOs(utxos...)
	if err != nil {
		return err
	}
	// put half of the satoshis back into the original address
	// the rest will be returned via the change output
	err = consolidationTx.PayToAddress(consolidationAddress, totalSatoshis/2)
	if err != nil {
		return err
	}
	err = consolidationTx.ChangeToAddress(consolidationAddress, b.FeeQuote)
	if err != nil {
		return err
	}

	unlockerGetter := unlocker.Getter{PrivateKey: b.ToKeySet.PrivateKey}
	if err = consolidationTx.FillAllInputs(context.Background(), &unlockerGetter); err != nil {
		return err
	}

	if err = b.ProcessTransaction(ctx, consolidationTx); err != nil {
		return err
	}

	return nil
}

func (b *Broadcaster) Run(ctx context.Context, sendOnChannel *int) error {

	fundingTx := b.NewFundingTransaction()
	log.Printf("funding tx: %s\n", fundingTx.TxID())

	_, err := b.Client.PutTransaction(ctx, fundingTx)
	if err != nil {
		return err
	}

	for i := int64(0); i < b.Outputs; i++ {
		u := &bt.UTXO{
			TxID:          fundingTx.TxIDBytes(),
			Vout:          uint32(i),
			LockingScript: fundingTx.Outputs[i].LockingScript,
			Satoshis:      fundingTx.Outputs[i].Satoshis,
		}
		b.txs[i], _ = b.NewTransaction(b.ToKeySet, u)
		log.Printf("generated tx: %s\n", b.txs[i].TxID())
	}

	// let the node recover
	time.Sleep(1 * time.Second)

	var wg sync.WaitGroup
	if b.IsDryRun {
		for i, tx := range b.txs {
			log.Printf("Processing tx %d / %d\n", i+1, len(b.txs))
			if err = b.ProcessTransaction(ctx, tx); err != nil {
				return err
			}
		}
	} else if sendOnChannel != nil && *sendOnChannel > 0 {
		limit := make(chan bool, *sendOnChannel)
		for i, tx := range b.txs {
			wg.Add(1)
			limit <- true
			go func(i int, tx *bt.Tx) {
				defer func() {
					wg.Done()
					<-limit
				}()

				log.Printf("Processing tx %d / %d\n", i+1, len(b.txs))

				if err = b.ProcessTransaction(ctx, tx); err != nil {
					panic(err)
				}
			}(i, tx)
		}
		wg.Wait()
	} else {
		for i, tx := range b.txs {
			wg.Add(1)
			go func(i int, tx *bt.Tx) {
				defer wg.Done()

				log.Printf("Processing tx %d / %d\n", i+1, len(b.txs))
				if err = b.ProcessTransaction(ctx, tx); err != nil {
					panic(err)
				}
			}(i, tx)
		}
		wg.Wait()
	}

	return nil
}

func (b *Broadcaster) ProcessTransaction(ctx context.Context, tx *bt.Tx) error {
	ctxClient, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	res, err := b.Client.PutTransaction(ctxClient, tx)
	if err != nil {
		return err
	}
	if res.TimedOut {
		log.Printf("res %s: %#v (TIMEOUT)\n", tx.TxID(), res.Status.String())
	} else if res.RejectReason != "" {
		log.Printf("res %s: %#v (REJECT: %s)\n", tx.TxID(), res.Status.String(), res.RejectReason)
	} else {
		log.Printf("res %s: %#v\n", tx.TxID(), res.Status.String())
	}

	return nil
}

func (b *Broadcaster) NewFundingTransaction() *bt.Tx {
	var fq = bt.NewFeeQuote()
	fq.AddQuote(bt.FeeTypeStandard, stdFee)
	fq.AddQuote(bt.FeeTypeData, dataFee)

	var err error
	addr := b.FromKeySet.Address(!b.IsRegtest)

	tx := bt.NewTx()

	if b.IsRegtest {
		var txid string
		var vout uint32
		var scriptPubKey string
		for {
			// create the first funding transaction
			txid, vout, scriptPubKey, err = b.SendToAddress(addr, 100_000_000)
			if err == nil {
				log.Printf("init tx: %s:%d\n", txid, vout)
				break
			}

			log.Printf(".")
			time.Sleep(100 * time.Millisecond)
		}
		err = tx.From(txid, vout, scriptPubKey, 100_000_000)
		if err != nil {
			panic(err)
		}
	} else {
		// live mode, we need to get the utxos from woc
		var utxos []*bt.UTXO
		utxos, err = b.FromKeySet.GetUTXOs(!b.IsRegtest)
		if err != nil {
			panic(err)
		}
		if len(utxos) == 0 {
			panic("no utxos for address: " + addr)
		}

		// this consumes all the utxos from the key
		err = tx.FromUTXOs(utxos...)
		if err != nil {
			panic(err)
		}
	}

	estimateFee := fees.EstimateFee(uint64(stdFee.MiningFee.Satoshis), 1, 1)
	for i := int64(0); i < b.Outputs; i++ {
		// we send triple the fee to the output address
		// this will allow us to send the change back to the original address
		_ = tx.PayTo(b.ToKeySet.Script, estimateFee*3)
	}

	_ = tx.Change(b.FromKeySet.Script, fq)

	unlockerGetter := unlocker.Getter{PrivateKey: b.FromKeySet.PrivateKey}
	if err = tx.FillAllInputs(context.Background(), &unlockerGetter); err != nil {
		panic(err)
	}

	return tx
}

func (b *Broadcaster) NewTransaction(key *keyset.KeySet, useUtxo *bt.UTXO) (*bt.Tx, *bt.UTXO) {
	var err error

	tx := bt.NewTx()
	_ = tx.FromUTXOs(useUtxo)
	// the output value of the utxo should be exactly 3x the fee
	// in this way we can consolidate the output back into the original address
	// even for the smallest transactions with only 1 output
	_ = tx.PayTo(key.Script, 2*useUtxo.Satoshis/3)
	_ = tx.Change(key.Script, b.FeeQuote)

	unlockerGetter := unlocker.Getter{PrivateKey: key.PrivateKey}
	if err = tx.FillAllInputs(context.Background(), &unlockerGetter); err != nil {
		panic(err)
	}

	// set the utxo to use for the next transaction
	changeVout := len(tx.Outputs) - 1
	useUtxo = &bt.UTXO{
		TxID:          tx.TxIDBytes(),
		Vout:          uint32(changeVout),
		LockingScript: tx.Outputs[changeVout].LockingScript,
		Satoshis:      tx.Outputs[changeVout].Satoshis,
	}

	return tx, useUtxo
}

// SendToAddress Let the bitcoin node in regtest mode send some bitcoin to our address
func (b *Broadcaster) SendToAddress(address string, satoshis uint64) (string, uint32, string, error) {
	rpcURL, err, found := gocore.Config().GetURL("peer_rpc")
	if !found {
		log.Fatalf("Could not find peer_rpc in config: %v", err)
	}
	if err != nil {
		log.Fatalf("Could not parse peer_rpc: %v", err)
	}

	// we are only in dry run mode and will not actually send anything
	if b.IsDryRun {
		txid := random.String(64, random.Hex)
		pubKeyHash := base58.Decode(address)
		scriptPubKey := "76a914" + hex.EncodeToString(pubKeyHash[1:21]) + "88ac"
		return txid, 0, scriptPubKey, nil
	}

	client, err := bitcoin.NewFromURL(rpcURL, false)
	if err != nil {
		log.Fatalf("Could not create bitcoin client: %v", err)
	}

	amount := float64(satoshis) / float64(1e8)

	txid, err := client.SendToAddress(address, amount)
	if err != nil {
		return "", 0, "", err
	}

	tx, err := client.GetRawTransaction(txid)
	if err != nil {
		return "", 0, "", err
	}

	for i, vout := range tx.Vout {
		if vout.ScriptPubKey.Addresses[0] == address {
			return txid, uint32(i), vout.ScriptPubKey.Hex, nil
		}
	}

	return "", 0, "", errors.New("address not found in tx")
}
