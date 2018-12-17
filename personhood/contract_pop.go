package personhood

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dedis/cothority"
	"github.com/dedis/cothority/byzcoin"
	"github.com/dedis/cothority/byzcoin/contracts"
	"github.com/dedis/cothority/darc"
	"github.com/dedis/onet/network"
	"github.com/dedis/protobuf"
)

// ContractPopParty represents a pop-party that holds either a configuration
// or a final statement.
var ContractPopParty = "popParty"

type contract struct {
	byzcoin.BasicContract
	PopPartyInstance
}

func contractPopPartyFromBytes(in []byte) (byzcoin.Contract, error) {
	c := &contract{}
	err := protobuf.DecodeWithConstructors(in, &c.PopPartyInstance, network.DefaultConstructors(cothority.Suite))
	if err != nil {
		return nil, errors.New("couldn't unmarshal existing PopPartyInstance: " + err.Error())
	}
	return c, nil
}

func (c *contract) Spawn(rst byzcoin.ReadOnlyStateTrie, inst byzcoin.Instruction, coins []byzcoin.Coin) (scs []byzcoin.StateChange, cout []byzcoin.Coin, err error) {
	cout = coins

	fsBuf := inst.Spawn.Args.Search("finalStatement")
	if fsBuf == nil {
		return nil, nil, errors.New("need FinalStatement argument")
	}
	darcID := inst.Spawn.Args.Search("darcID")
	if darcID == nil {
		return nil, nil, errors.New("no darcID argument")
	}
	c.State = 1

	var fs FinalStatement
	err = protobuf.DecodeWithConstructors(fsBuf, &fs, network.DefaultConstructors(cothority.Suite))
	if err != nil {
		return nil, nil, errors.New("couldn't unmarshal the final statement: " + err.Error())
	}
	c.FinalStatement = &fs

	value, _, _, _, err := rst.GetValues(darcID)
	if err != nil {
		return nil, nil, errors.New("couldn't get darc in charge: " + err.Error())
	}
	d, err := darc.NewFromProtobuf(value)
	if err != nil {
		return nil, nil, errors.New("couldn't get darc: " + err.Error())
	}
	expr := d.Rules.Get("invoke:finalize")
	c.Organizers = len(strings.Split(string(expr), "|"))

	ppiBuf, err := protobuf.Encode(&c.PopPartyInstance)
	if err != nil {
		return nil, nil, errors.New("couldn't marshal PopPartyInstance: " + err.Error())
	}

	scs = byzcoin.StateChanges{
		byzcoin.NewStateChange(byzcoin.Create, inst.DeriveID(""), inst.Spawn.ContractID, ppiBuf, darc.ID(inst.InstanceID[:])),
	}
	return
}

func (c *contract) Invoke(rst byzcoin.ReadOnlyStateTrie, inst byzcoin.Instruction, coins []byzcoin.Coin) (scs []byzcoin.StateChange, cout []byzcoin.Coin, err error) {
	cout = coins

	var darcID darc.ID
	_, _, _, darcID, err = rst.GetValues(inst.InstanceID.Slice())
	if err != nil {
		return nil, nil, errors.New("couldn't get instance data: " + err.Error())
	}

	switch inst.Invoke.Command {
	case "finalize":
		if c.State != 1 {
			return nil, nil, fmt.Errorf("can only finalize party with state 1, but current state is %d",
				c.State)
		}
		if inst.Signatures[0].Signer.Darc == nil {
			return nil, nil, errors.New("only darc-signers allowed for finalization")
		}

		attBuf := inst.Invoke.Args.Search("attendees")
		if attBuf == nil {
			return nil, nil, errors.New("missing argument: attendees")
		}
		var atts Attendees
		err = protobuf.DecodeWithConstructors(attBuf, &atts, network.DefaultConstructors(cothority.Suite))

		alreadySigned := false
		orgDarc := inst.Signatures[0].Signer.Darc.ID
		for _, f := range c.Finalizations {
			if f.Equal(orgDarc) {
				alreadySigned = true
				break
			}
		}

		if len(c.Finalizations) == 0 || alreadySigned {
			// Store first proposition of list of attendees or reset if the same
			// organizer submits again
			c.FinalStatement.Attendees = atts
			c.Finalizations = []darc.ID{orgDarc}
		} else {
			// Check if it is the same set of attendees or not
			same := true
			for i, att := range c.FinalStatement.Attendees.Keys {
				if !att.Equal(atts.Keys[i]) {
					same = false
				}
			}
			if same {
				c.Finalizations = append(c.Finalizations, orgDarc)
			} else {
				c.FinalStatement.Attendees = atts
				c.Finalizations = []darc.ID{orgDarc}
			}
		}

	case "addParty":
		return nil, nil, errors.New("not yet implemented")

	case "mine":
		lrs := inst.Invoke.Args.Search("lrs")
		if lrs == nil {
			return nil, nil, errors.New("need lrs argument")
		}

		coinIID := inst.Invoke.Args.Search("coinIID")
		if coinIID == nil {
			return nil, nil, errors.New("need coinIID argument")
		}
		coinBuf, _, cid, coinDarc, err := rst.GetValues(coinIID)
		if cid != contracts.ContractCoinID {
			return nil, nil, errors.New("coinIID is not a coin contract")
		}
		var coin byzcoin.Coin
		err = protobuf.Decode(coinBuf, &coin)
		if err != nil {
			return nil, nil, errors.New("couldn't unmarshal coin: " + err.Error())
		}
		err = coin.SafeAdd(c.MiningReward)
		if err != nil {
			return nil, nil, errors.New("couldn't add mining reward: " + err.Error())
		}
		coinBuf, err = protobuf.Encode(coin)
		scs = append(scs, byzcoin.NewStateChange(byzcoin.Update,
			byzcoin.NewInstanceID(coinIID),
			contracts.ContractCoinID, coinBuf, coinDarc))

	default:
		return nil, nil, errors.New("unknown command")
	}

	// Storing new version of PopPartyInstance
	ppiBuf, err := protobuf.Encode(&c.PopPartyInstance)
	if err != nil {
		return nil, nil, errors.New("couldn't marshal PopPartyInstance: " + err.Error())
	}

	// Update existing final statement
	scs = append(scs, byzcoin.NewStateChange(byzcoin.Update, inst.InstanceID, ContractPopParty, ppiBuf, darcID))

	return scs, coins, nil
}
