package byzcoin

import (
	"testing"

	"github.com/dedis/cothority/darc"
	"github.com/stretchr/testify/require"
)

func TestReplayGuard(t *testing.T) {
	// increment
	sst, err := newMemStagingStateTrie([]byte("my nonce"))
	require.NoError(t, err)
	signers := []darc.Signer{darc.NewSignerEd25519(nil, nil), darc.NewSignerEd25519(nil, nil)}
	var sigs []darc.Signature
	for _, signer := range signers {
		sigs = append(sigs, darc.Signature{
			Signature: []byte(""),
			Signer:    signer.Identity()},
		)
	}
	sc, err := incrementSignerCounters(sst, sigs)
	require.NoError(t, err)
	require.NoError(t, sst.StoreAll(sc))

	// check that they're 1 using getSignerCounter
	ctr0, err := getSignerCounter(sst, signers[0].Identity().String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), ctr0)

	ctr1, err := getSignerCounter(sst, signers[1].Identity().String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), ctr1)

	// increment again, now the counter state is at 2
	sc, err = incrementSignerCounters(sst, sigs)
	require.NoError(t, err)
	require.NoError(t, sst.StoreAll(sc))

	// verify, the new counter state must be 3
	err = verifySignerCounters(sst, []uint64{3, 3}, sigs)
	require.NoError(t, err)
}
