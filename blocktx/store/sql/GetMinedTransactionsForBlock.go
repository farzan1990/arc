package sql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/bitcoin-sv/arc/blocktx/blocktx_api"
	"github.com/ordishs/gocore"
)

// GetMinedTransactionsForBlock returns the transaction hashes for a given block hash and source
func (s *SQL) GetMinedTransactionsForBlock(ctx context.Context, blockAndSource *blocktx_api.BlockAndSource) (*blocktx_api.MinedTransactions, error) {
	start := gocore.CurrentNanos()
	defer func() {
		gocore.NewStat("blocktx").NewStat("GetMinedTransactionsForBlock").AddTime(start)
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	qBlock := `
		SELECT
		 b.hash
		,b.height
		,b.merkleroot
		,b.prevhash
		,b.orphanedyn
		,b.processed_at
		FROM blocks b
		WHERE b.hash = $1
	`

	// TODO check if index required
	qTransactions := `
		SELECT
		 t.hash
		FROM transactions t
		INNER JOIN block_transactions_map m ON m.txid = t.id
		INNER JOIN blocks b ON m.blockid = b.id
		WHERE b.hash = $1
		AND t.source = $2
	`

	var block blocktx_api.Block

	var processed_at sql.NullString

	if err := s.db.QueryRowContext(ctx, qBlock, blockAndSource.Hash).Scan(
		&block.Hash,
		&block.Height,
		&block.MerkleRoot,
		&block.PreviousHash,
		&block.Orphaned,
		&processed_at,
	); err != nil {
		return nil, err
	}

	block.Processed = processed_at.Valid

	rows, err := s.db.QueryContext(ctx, qTransactions, blockAndSource.Hash, blockAndSource.Source)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	defer rows.Close()

	var hash []byte
	transactions := make([]*blocktx_api.Transaction, 0)

	for rows.Next() {
		err = rows.Scan(&hash)
		if err != nil {
			return nil, err
		}

		transactions = append(transactions, &blocktx_api.Transaction{
			Hash: hash,
		})
	}

	return &blocktx_api.MinedTransactions{
		Block:        &block,
		Transactions: transactions,
	}, nil
}
