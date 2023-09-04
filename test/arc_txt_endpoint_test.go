package test

import (
	"bytes"
	"context"
	"fmt"
	"github.com/bitcoinsv/bsvd/bsvec"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/libsv/go-bk/bec"
	"github.com/libsv/go-bt/v2"
	"github.com/libsv/go-bt/v2/bscript"
	"github.com/libsv/go-bt/v2/unlocker"
	"log"
	"net/http"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	info, err := bitcoind.GetInfo()
	if err != nil {
		log.Fatalf("failed to get info: %v", err)
	}

	log.Printf("current block height: %d", info.Blocks)

	os.Exit(m.Run())
}

func TestHttpPost(t *testing.T) {
	address, privateKey := getNewWalletAddress(t)

	fmt.Println(address)

	sendToAddress(t, address, 0.01)
	txID := sendToAddress(t, address, 0.02)
	hash := generate(t, 1, address)

	fmt.Println(txID)
	fmt.Println(hash)

	utxos := getUnspentUtxos(t, address)
	if len(utxos) == 0 {
		log.Fatal("No UTXOs available for the address")
	}

	tx := bt.NewTx()

	// Add an input using the first UTXO
	utxo := utxos[0]
	utxoTxID := utxo.Txid
	utxoVout := uint32(utxo.Vout)
	utxoSatoshis := uint64(utxo.Amount * 1e8) // Convert BTC to satoshis
	utxoScript := utxo.ScriptPubKey

	err := tx.From(utxoTxID, utxoVout, utxoScript, utxoSatoshis)
	if err != nil {
		log.Fatalf("Failed adding input: %v", err)
	}

	// Add an output to the address you've previously created
	recipientAddress := address
	amountToSend := uint64(1) // Example value - 0.009 BTC (taking fees into account)

	recipientScript, err := bscript.NewP2PKHFromAddress(recipientAddress)
	if err != nil {
		log.Fatalf("Failed converting address to script: %v", err)
	}

	err = tx.PayTo(recipientScript, amountToSend)
	if err != nil {
		log.Fatalf("Failed adding output: %v", err)
	}

	// Sign the input

	wif, err := btcutil.DecodeWIF(privateKey)
	if err != nil {
		log.Fatalf("Failed to decode WIF: %v", err)
	}

	// Extract raw private key bytes directly from the WIF structure
	privateKeyDecoded := wif.PrivKey.Serialize()

	pk, _ := bec.PrivKeyFromBytes(bsvec.S256(), privateKeyDecoded)
	unlockerGetter := unlocker.Getter{PrivateKey: pk}
	err = tx.FillAllInputs(context.Background(), &unlockerGetter)
	if err != nil {
		log.Fatalf("sign failed: %v", err)
	}

	extBytes := tx.ExtendedBytes()

	// Print or work with the extended bytes as required
	fmt.Printf("Extended Bytes: %x\n", extBytes)
	fmt.Println(extBytes)

	url := "http://arc:9090/arc/v1/txs"

	// The request body data.
	// data := []byte("{}")

	// Create a new request using http.
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(extBytes))

	// If there is an error while creating the request, fail the test.
	if err != nil {
		t.Fatalf("Error creating HTTP request: %s", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "text/plain")

	// Send the request using http.Client.
	client := &http.Client{}
	resp, err := client.Do(req)

	// If there is an error while sending the request, fail the test.
	if err != nil {
		t.Fatalf("Error sending HTTP request: %s", err)
	}

	defer resp.Body.Close()

	// Check the HTTP status code.
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK, got: %s", resp.Status)
	}
}
