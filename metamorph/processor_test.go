package metamorph

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/TAAL-GmbH/arc/blocktx/blocktx_api"
	"github.com/TAAL-GmbH/arc/callbacker/callbacker_api"
	"github.com/TAAL-GmbH/arc/metamorph/metamorph_api"
	"github.com/TAAL-GmbH/arc/metamorph/store"
	"github.com/TAAL-GmbH/arc/metamorph/store/badger"
	"github.com/TAAL-GmbH/arc/metamorph/store/sql"
	"github.com/TAAL-GmbH/arc/p2p"
	"github.com/TAAL-GmbH/arc/test"
	"github.com/labstack/gommon/random"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProcessor(t *testing.T) {
	t.Run("NewProcessor", func(t *testing.T) {
		s, err := sql.New("sqlite_memory")
		require.NoError(t, err)

		pm := p2p.NewPeerManagerMock()

		processor := NewProcessor(1, s, pm, "test", nil, nil)
		if processor == nil {
			t.Error("Expected a non-nil Processor")
		}
	})

	t.Run("NewProcessor - err no store", func(t *testing.T) {
		defer func() {
			r := recover()
			assert.Equal(t, "store cannot be nil", r.(string))
		}()

		_ = NewProcessor(1, nil, nil, "test", nil, nil)
	})

	t.Run("NewProcessor - err no peer manager", func(t *testing.T) {
		defer func() {
			r := recover()
			assert.Equal(t, "peer manager cannot be nil", r.(string))
		}()

		s, err := sql.New("sqlite_memory")
		require.NoError(t, err)

		processor := NewProcessor(1, s, nil, "test", nil, nil)
		if processor == nil {
			t.Error("Expected a non-nil Processor")
		}
	})
}

func TestLoadUnseen(t *testing.T) {
	t.Run("LoadUnseen empty", func(t *testing.T) {
		s, err := sql.New("sqlite_memory")
		require.NoError(t, err)

		pm := p2p.NewPeerManagerMock()

		processor := NewProcessor(1, s, pm, "test", nil, nil)
		assert.Equal(t, 0, processor.tx2ChMap.Len())
		processor.LoadUnseen()
		assert.Equal(t, 0, processor.tx2ChMap.Len())
	})

	t.Run("LoadUnseen", func(t *testing.T) {
		s, err := sql.New("sqlite_memory")
		require.NoError(t, err)
		setStoreTestData(t, s)

		pm := p2p.NewPeerManagerMock()

		processor := NewProcessor(1, s, pm, "test", nil, nil)
		assert.Equal(t, 0, processor.tx2ChMap.Len())
		processor.LoadUnseen()
		assert.Equal(t, 2, processor.tx2ChMap.Len())
		items := processor.tx2ChMap.Items()
		assert.Equal(t, test.TX1Bytes, items[test.TX1].Hash)
		assert.Equal(t, test.TX2Bytes, items[test.TX2].Hash)
	})
}

func TestProcessTransaction(t *testing.T) {
	t.Run("ProcessTransaction", func(t *testing.T) {
		s, err := sql.New("sqlite_memory")
		require.NoError(t, err)

		pm := p2p.NewPeerManagerMock()

		registerCh := make(chan *blocktx_api.TransactionAndSource)

		var wgRegister sync.WaitGroup
		wgRegister.Add(1)
		go func() {
			for tx := range registerCh {
				assert.Equal(t, test.TX1Bytes, tx.Hash)
				assert.Equal(t, "test", tx.Source)
				wgRegister.Done()
			}
		}()

		processor := NewProcessor(1, s, pm, "test", registerCh, nil)
		assert.Equal(t, 0, processor.tx2ChMap.Len())

		expectedResponses := []metamorph_api.Status{
			metamorph_api.Status_RECEIVED,
			metamorph_api.Status_STORED,
			metamorph_api.Status_ANNOUNCED_TO_NETWORK,
		}

		responseChannel := make(chan StatusAndError)

		var wg sync.WaitGroup
		wg.Add(len(expectedResponses))
		go func() {
			n := 0
			for response := range responseChannel {
				status := response.Status
				assert.Equal(t, test.TX1Bytes, response.Hash)
				assert.Equalf(t, expectedResponses[n], status, "Iteration %d: Expected %s, got %s", n, expectedResponses[n].String(), status.String())
				wg.Done()
				n++
			}
		}()

		processor.ProcessTransaction(NewProcessorRequest(
			context.Background(),
			&store.StoreData{
				Hash: test.TX1Bytes,
			},
			responseChannel,
		))
		wg.Wait()
		wgRegister.Wait()

		assert.Equal(t, 1, processor.tx2ChMap.Len())
		items := processor.tx2ChMap.Items()
		assert.Equal(t, test.TX1Bytes, items[test.TX1].Hash)
		assert.Equal(t, metamorph_api.Status_ANNOUNCED_TO_NETWORK, items[test.TX1].status)

		assert.Len(t, pm.Announced, 1)
		assert.Equal(t, test.TX1Bytes, pm.Announced[0])

		txStored, err := s.Get(context.Background(), test.TX1Bytes)
		require.NoError(t, err)
		assert.Equal(t, test.TX1Bytes, txStored.Hash)
	})
}

func TestSendStatusForTransaction(t *testing.T) {
	t.Run("SendStatusForTransaction unknown tx", func(t *testing.T) {
		s, err := sql.New("sqlite_memory")
		require.NoError(t, err)

		pm := p2p.NewPeerManagerMock()

		processor := NewProcessor(1, s, pm, "test", nil, nil)
		assert.Equal(t, 0, processor.tx2ChMap.Len())

		ok, sendErr := processor.SendStatusForTransaction(test.TX1, metamorph_api.Status_SEEN_ON_NETWORK, nil)
		assert.False(t, ok)
		assert.NoError(t, sendErr)
		assert.Equal(t, 0, processor.tx2ChMap.Len())
	})

	t.Run("SendStatusForTransaction err", func(t *testing.T) {
		s, err := sql.New("sqlite_memory")
		require.NoError(t, err)
		setStoreTestData(t, s)

		pm := p2p.NewPeerManagerMock()

		processor := NewProcessor(1, s, pm, "test", nil, nil)
		assert.Equal(t, 0, processor.tx2ChMap.Len())

		throwErr := fmt.Errorf("some error")
		ok, sendErr := processor.SendStatusForTransaction(test.TX1, metamorph_api.Status_REJECTED, throwErr)
		assert.True(t, ok)
		assert.NoError(t, sendErr)
		assert.Equal(t, 0, processor.tx2ChMap.Len())

		txStored, err := s.Get(context.Background(), test.TX1Bytes)
		require.NoError(t, err)
		assert.Equal(t, metamorph_api.Status_REJECTED, txStored.Status)
		assert.Equal(t, throwErr.Error(), txStored.RejectReason)
	})

	t.Run("SendStatusForTransaction invalid tx id", func(t *testing.T) {
		s, err := sql.New("sqlite_memory")
		require.NoError(t, err)

		pm := p2p.NewPeerManagerMock()

		processor := NewProcessor(1, s, pm, "test", nil, nil)
		assert.Equal(t, 0, processor.tx2ChMap.Len())

		ok, sendErr := processor.SendStatusForTransaction("test", metamorph_api.Status_REJECTED, nil)
		assert.False(t, ok)
		assert.ErrorIs(t, sendErr, hex.InvalidByteError('t'))
	})

	t.Run("SendStatusForTransaction known tx", func(t *testing.T) {
		s, err := sql.New("sqlite_memory")
		require.NoError(t, err)
		setStoreTestData(t, s)

		pm := p2p.NewPeerManagerMock()

		processor := NewProcessor(1, s, pm, "test", nil, nil)
		assert.Equal(t, 0, processor.tx2ChMap.Len())

		ok, sendErr := processor.SendStatusForTransaction(test.TX1, metamorph_api.Status_MINED, nil)
		assert.True(t, ok)
		assert.NoError(t, sendErr)
		assert.Equal(t, 0, processor.tx2ChMap.Len())

		txStored, err := s.Get(context.Background(), test.TX1Bytes)
		require.NoError(t, err)
		assert.Equal(t, metamorph_api.Status_MINED, txStored.Status)
	})

	t.Run("SendStatusForTransaction known tx - no update", func(t *testing.T) {
		s, err := sql.New("sqlite_memory")
		require.NoError(t, err)
		setStoreTestData(t, s)

		pm := p2p.NewPeerManagerMock()

		processor := NewProcessor(1, s, pm, "test", nil, nil)
		assert.Equal(t, 0, processor.tx2ChMap.Len())

		ok, sendErr := processor.SendStatusForTransaction(test.TX1, metamorph_api.Status_ANNOUNCED_TO_NETWORK, nil)
		assert.False(t, ok)
		assert.NoError(t, sendErr)
		assert.Equal(t, 0, processor.tx2ChMap.Len())

		txStored, err := s.Get(context.Background(), test.TX1Bytes)
		require.NoError(t, err)
		assert.Equal(t, metamorph_api.Status_SENT_TO_NETWORK, txStored.Status)
	})

	t.Run("SendStatusForTransaction known tx - processed", func(t *testing.T) {
		s, err := sql.New("sqlite_memory")
		require.NoError(t, err)

		pm := p2p.NewPeerManagerMock()

		processor := NewProcessor(1, s, pm, "test", nil, nil)
		assert.Equal(t, 0, processor.tx2ChMap.Len())

		responseChannel := make(chan StatusAndError)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			for response := range responseChannel {
				status := response.Status
				fmt.Printf("response: %s\n", status)
				if status == metamorph_api.Status_ANNOUNCED_TO_NETWORK {
					close(responseChannel)
					wg.Done()
					return
				}
			}
		}()

		processor.ProcessTransaction(NewProcessorRequest(
			context.Background(),
			&store.StoreData{
				Hash: test.TX1Bytes,
			},
			responseChannel,
		))
		wg.Wait()

		assert.Equal(t, 1, processor.tx2ChMap.Len())

		ok, sendErr := processor.SendStatusForTransaction(test.TX1, metamorph_api.Status_SEEN_ON_NETWORK, nil)
		assert.False(t, ok)
		assert.NoError(t, sendErr)
		assert.Equal(t, 0, processor.tx2ChMap.Len(), "should have been removed from the map")

		txStored, err := s.Get(context.Background(), test.TX1Bytes)
		require.NoError(t, err)
		assert.Equal(t, metamorph_api.Status_SEEN_ON_NETWORK, txStored.Status)
	})
}

func TestSendStatusMinedForTransaction(t *testing.T) {
	t.Run("SendStatusMinedForTransaction unknown tx", func(t *testing.T) {
		s, err := sql.New("sqlite_memory")
		require.NoError(t, err)

		pm := p2p.NewPeerManagerMock()

		processor := NewProcessor(1, s, pm, "test", nil, nil)
		assert.Equal(t, 0, processor.tx2ChMap.Len())

		ok, sendErr := processor.SendStatusMinedForTransaction(test.TX1Bytes, []byte("hash1"), 1233)
		assert.True(t, ok)
		assert.NoError(t, sendErr)
	})

	t.Run("SendStatusMinedForTransaction known tx", func(t *testing.T) {
		s, err := sql.New("sqlite_memory")
		require.NoError(t, err)
		setStoreTestData(t, s)

		pm := p2p.NewPeerManagerMock()

		processor := NewProcessor(1, s, pm, "test", nil, nil)
		assert.Equal(t, 0, processor.tx2ChMap.Len())

		ok, sendErr := processor.SendStatusMinedForTransaction(test.TX1Bytes, []byte("hash1"), 1233)
		assert.True(t, ok)
		assert.NoError(t, sendErr)
		assert.Equal(t, 0, processor.tx2ChMap.Len())

		txStored, err := s.Get(context.Background(), test.TX1Bytes)
		require.NoError(t, err)
		assert.Equal(t, metamorph_api.Status_MINED, txStored.Status)
	})

	t.Run("SendStatusMinedForTransaction callback", func(t *testing.T) {
		s, err := sql.New("sqlite_memory")
		require.NoError(t, err)
		setStoreTestData(t, s)

		pm := p2p.NewPeerManagerMock()

		var wg sync.WaitGroup
		callbackCh := make(chan *callbacker_api.Callback)
		wg.Add(1)
		go func() {
			for cb := range callbackCh {
				assert.Equal(t, metamorph_api.Status_MINED, metamorph_api.Status(cb.Status))
				assert.Equal(t, test.TX1Bytes, cb.Hash)
				assert.Equal(t, []byte("hash1"), cb.BlockHash)
				assert.Equal(t, uint64(1233), cb.BlockHeight)
				assert.Equal(t, "https://test.com", cb.Url)
				assert.Equal(t, "token", cb.Token)
				wg.Done()
			}
		}()

		processor := NewProcessor(1, s, pm, "test", nil, callbackCh)

		ok, sendErr := processor.SendStatusMinedForTransaction(test.TX1Bytes, []byte("hash1"), 1233)
		assert.True(t, ok)
		assert.NoError(t, sendErr)

		wg.Wait()
	})

	t.Run("SendStatusForTransaction known tx - processed", func(t *testing.T) {
		s, err := sql.New("sqlite_memory")
		require.NoError(t, err)

		pm := p2p.NewPeerManagerMock()

		processor := NewProcessor(1, s, pm, "test", nil, nil)
		assert.Equal(t, 0, processor.tx2ChMap.Len())

		responseChannel := make(chan StatusAndError)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			for response := range responseChannel {
				status := response.Status
				fmt.Printf("response: %s\n", status)
				if status == metamorph_api.Status_ANNOUNCED_TO_NETWORK {
					close(responseChannel)
					time.Sleep(1 * time.Millisecond)
					wg.Done()
					return
				}
			}
		}()

		processor.ProcessTransaction(NewProcessorRequest(
			context.Background(),
			&store.StoreData{
				Hash: test.TX1Bytes,
			},
			responseChannel,
		))
		wg.Wait()

		assert.Equal(t, 1, processor.tx2ChMap.Len())

		ok, sendErr := processor.SendStatusMinedForTransaction(test.TX1Bytes, []byte("hash1"), 1233)
		assert.True(t, ok)
		assert.NoError(t, sendErr)
		assert.Equal(t, 0, processor.tx2ChMap.Len(), "should have been removed from the map")

		txStored, err := s.Get(context.Background(), test.TX1Bytes)
		require.NoError(t, err)
		assert.Equal(t, metamorph_api.Status_MINED, txStored.Status)
	})
}

func BenchmarkProcessTransaction(b *testing.B) {
	direName := fmt.Sprintf("./test-benchmark-%s", random.String(6))
	s, err := badger.New(direName)
	require.NoError(b, err)
	defer func() {
		_ = s.Close(context.Background())
		_ = os.RemoveAll(direName)
	}()

	pm := p2p.NewPeerManagerMock()
	processor := NewProcessor(16, s, pm, "test", nil, nil)
	processor.SetLogger(p2p.TestLogger{})
	assert.Equal(b, 0, processor.tx2ChMap.Len())

	txs := make(map[string][]byte)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		txID := fmt.Sprintf("%x", i)
		txHash := []byte(txID)
		txs[txID] = txHash
		processor.ProcessTransaction(NewProcessorRequest(
			context.Background(),
			&store.StoreData{
				Hash:   txHash,
				Status: metamorph_api.Status_UNKNOWN,
				RawTx:  test.TX1RawBytes,
			},
			nil,
		))
	}
	b.StopTimer()

	// wait for the last items to be written to the store
	time.Sleep(1 * time.Second)
}
