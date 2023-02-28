package tests

import (
	"context"
	"testing"

	"github.com/TAAL-GmbH/arc/metamorph/metamorph_api"
	"github.com/TAAL-GmbH/arc/metamorph/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func UpdateStatus(t *testing.T, s store.MetamorphStore) {
	err := s.Set(context.Background(), Tx1Bytes, &store.StoreData{
		Hash:   Tx1Bytes,
		Status: metamorph_api.Status_ANNOUNCED_TO_NETWORK,
	})
	require.NoError(t, err)

	var data *store.StoreData
	data, err = s.Get(context.Background(), Tx1Bytes)
	require.NoError(t, err)

	assert.Equal(t, metamorph_api.Status_ANNOUNCED_TO_NETWORK, data.Status)
	assert.Equal(t, "", data.RejectReason)
	assert.Equal(t, []byte(nil), data.BlockHash)
	assert.Equal(t, uint64(0), data.BlockHeight)

	err = s.UpdateStatus(context.Background(), Tx1Bytes, metamorph_api.Status_SENT_TO_NETWORK, "")
	require.NoError(t, err)

	data, err = s.Get(context.Background(), Tx1Bytes)
	require.NoError(t, err)
	assert.Equal(t, metamorph_api.Status_SENT_TO_NETWORK, data.Status)
	assert.Equal(t, "", data.RejectReason)
	assert.Equal(t, []byte(nil), data.BlockHash)
	assert.Equal(t, uint64(0), data.BlockHeight)
}

func UpdateStatusWithError(t *testing.T, s store.MetamorphStore) {
	err := s.Set(context.Background(), Tx1Bytes, &store.StoreData{
		Hash:   Tx1Bytes,
		Status: metamorph_api.Status_ANNOUNCED_TO_NETWORK,
	})
	require.NoError(t, err)

	var data *store.StoreData
	data, err = s.Get(context.Background(), Tx1Bytes)
	require.NoError(t, err)

	assert.Equal(t, metamorph_api.Status_ANNOUNCED_TO_NETWORK, data.Status)
	assert.Equal(t, "", data.RejectReason)

	err = s.UpdateStatus(context.Background(), Tx1Bytes, metamorph_api.Status_REJECTED, "error encountered")
	require.NoError(t, err)

	data, err = s.Get(context.Background(), Tx1Bytes)
	require.NoError(t, err)
	assert.Equal(t, metamorph_api.Status_REJECTED, data.Status)
	assert.Equal(t, "error encountered", data.RejectReason)
}
