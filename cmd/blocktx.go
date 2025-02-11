package cmd

import (
	"github.com/bitcoin-sv/arc/blocktx"
	"github.com/ordishs/go-utils"
	"github.com/ordishs/gocore"
)

func StartBlockTx(logger utils.Logger) (func(), error) {
	dbMode, _ := gocore.Config().Get("blocktx_dbMode", "sqlite")

	// dbMode can be sqlite, sqlite_memory or postgres
	blockStore, err := blocktx.NewStore(dbMode)
	if err != nil {
		logger.Fatalf("Error creating blocktx store: %v", err)
	}

	blockNotifier := blocktx.NewBlockNotifier(blockStore, logger)

	blockTxServer := blocktx.NewServer(blockStore, blockNotifier, logger)

	go func() {
		if err = blockTxServer.StartGRPCServer(); err != nil {
			logger.Fatalf("%v", err)
		}
	}()

	return func() {
		logger.Infof("Shutting down blocktx store")
		err = blockStore.Close()
		if err != nil {
			logger.Errorf("Error closing blocktx store: %v", err)
		}
	}, nil
}
