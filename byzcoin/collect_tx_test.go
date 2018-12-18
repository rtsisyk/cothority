package byzcoin

import (
	"fmt"
	"github.com/dedis/onet/log"
	"testing"
	"time"

	"github.com/dedis/cothority"
	"github.com/dedis/cothority/skipchain"
	"github.com/dedis/onet"
	"github.com/dedis/onet/network"
	"github.com/stretchr/testify/require"
)

var testSuite = cothority.Suite

func TestCollectTx(t *testing.T) {
	protoPrefix := "TestCollectTx"
	getTx := func(leader *network.ServerIdentity, roster *onet.Roster, scID skipchain.SkipBlockID, latestID skipchain.SkipBlockID) []ClientTransaction {
		tx := ClientTransaction{
			Instructions: []Instruction{Instruction{}},
		}
		return []ClientTransaction{tx}
	}
	for _, n := range []int{2, 3, 10} {
		protoName := fmt.Sprintf("%s_%d", protoPrefix, n)
		_, err := onet.GlobalProtocolRegister(protoName, NewCollectTxProtocol(getTx))
		require.NoError(t, err)

		local := onet.NewLocalTest(testSuite)
		_, _, tree := local.GenBigTree(n, n, n-1, true)

		p, err := local.CreateProtocol(protoName, tree)
		require.NoError(t, err)

		root := p.(*CollectTxProtocol)
		root.SkipchainID = skipchain.SkipBlockID("hello")
		root.LatestID = skipchain.SkipBlockID("goodbye")
		require.NoError(t, root.Start())

		var txs []ClientTransaction
	outer:
		for {
			select {
			case newTxs, more := <-root.TxsChan:
				if more {
					txs = append(txs, newTxs...)
				} else {
					break outer
				}
			}
		}
		require.Equal(t, n, len(txs))
		local.CloseAll()
	}
}

func TestCollectTxFail(t *testing.T) {
	protoName := "TestCollectTx"
	getTx := func(leader *network.ServerIdentity, roster *onet.Roster, scID skipchain.SkipBlockID, latestID skipchain.SkipBlockID) []ClientTransaction {
		tx := ClientTransaction{
			Instructions: []Instruction{Instruction{}},
		}
		return []ClientTransaction{tx}
	}
	_, err := onet.GlobalProtocolRegister(protoName, NewCollectTxProtocol(getTx))
	require.NoError(t, err)

	n := 3

	local := onet.NewLocalTest(testSuite)
	servers, _, tree := local.GenBigTree(n, n, n-1, true)

	log.Lvl1("Pausing second server and trying to get update")
	servers[1].Pause()
	p, err := local.CreateProtocol(protoName, tree)
	require.NoError(t, err)

	root := p.(*CollectTxProtocol)
	root.SkipchainID = skipchain.SkipBlockID("hello")
	root.LatestID = skipchain.SkipBlockID("goodbye")
	require.NoError(t, root.Start())

	closed := false
	var txs []ClientTransaction
outer:
	for {
		select {
		case newTxs, more := <-root.TxsChan:
			if more {
				txs = append(txs, newTxs...)
			} else {
				break outer
			}
		case <-time.After(time.Second):
			if !closed {
				close(root.Finish)
			} else {
				t.Fatal("timed out while waiting for results")
			}
		}
	}
	require.Equal(t, n-1, len(txs))
	local.CloseAll()
}
